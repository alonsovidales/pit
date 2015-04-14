package shardsmanager

import (
	"encoding/json"
	"fmt"
	"github.com/alonsovidales/pit/cfg"
	"github.com/alonsovidales/pit/log"
	"github.com/alonsovidales/pit/models/instances"
	"github.com/alonsovidales/pit/models/shard_info"
	"github.com/alonsovidales/pit/recommender"
	"io/ioutil"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	CRecPath       = "/rec"
	CGroupInfoPath = "/info"

	CRegenerateKey  = "/regenerate_shard_key"
	CChangeShardNum = "/change_shards_num"

	cMaxMinsToStore = 1440 // A day
)

type Manager struct {
	awsRegion      string
	s3BackupsPath  string
	port           int
	active         bool
	finished       bool
	acquiredShards map[string]recommender.RecommenderInt

	shardsModel    shardinfo.ModelInt
	instancesModel instances.InstancesModelInt
	reqSecStats    map[string]*statsReqSec
}

type statsReqSec struct {
	RecTreeStatus string   `json:"rec_tree_status"`
	BySecStats    []uint64 `json:"queries_by_sec"`
	ByMinStats    []uint64 `json:"queries_by_min"`
	queries       uint64
	inserts       uint64
	mutex         sync.Mutex
	stop          bool
}

func Init(prefix, awsRegion, s3BackupsPath string, port int) (mg *Manager) {
	mg = &Manager{
		s3BackupsPath: s3BackupsPath,
		port:          port,
		active:        true,
		finished:      false,
		reqSecStats:   make(map[string]*statsReqSec),

		shardsModel:    shardinfo.GetModel(prefix, awsRegion),
		instancesModel: instances.InitAndKeepAlive(prefix, awsRegion, true),
		awsRegion:      awsRegion,
		acquiredShards: make(map[string]recommender.RecommenderInt),
	}

	go mg.manage()

	return
}

func (mg *Manager) Stop() {
	mg.active = false
}

func (mg *Manager) IsFinished() bool {
	return mg.finished
}

func (mg *Manager) acquiredShard(group *shardinfo.GroupInfo) {
	rec := recommender.NewShard(mg.s3BackupsPath, group.GroupID, group.MaxElements, group.MaxScore, mg.awsRegion)
	rec.LoadBackup()
	rec.RecalculateTree()
	mg.acquiredShards[group.GroupID] = rec

	go mg.keepUpdateGroup(group.GroupID)
	log.Info("Finished acquisition of shard on group:", group.GroupID)
}

func (mg *Manager) keepUpdateGroup(groupID string) {
	for {
		gr := mg.shardsModel.GetGroupByID(groupID)
		if gr == nil || !gr.IsThisInstanceOwner() {
			delete(mg.acquiredShards, groupID)
			mg.reqSecStats[groupID].stop = true
			delete(mg.reqSecStats, groupID)
			log.Info("Shard released on group:", gr.GroupID)
			return
		}

		mg.acquiredShards[groupID].SetMaxElements(gr.MaxElements)
		mg.acquiredShards[groupID].SetMaxScore(gr.MaxScore)

		time.Sleep(time.Second)
	}
}

func (mg *Manager) recalculateRecs() {
	for {
		for _, rec := range mg.acquiredShards {
			if rec.IsDirty() {
				rec.RecalculateTree()
				rec.SaveBackup()
			}
		}

		time.Sleep(time.Second * 30)
	}
}

func (st *statsReqSec) monitorStats() {
	c := time.Tick(time.Second)
	i := 0
	for range c {
		st.BySecStats = append(st.BySecStats, st.queries)
		i++
		if i == 60 {
			i = 0
			v := uint64(0)
			for _, q := range st.BySecStats {
				v += q
			}
			st.ByMinStats = append(st.ByMinStats, v)
			if len(st.ByMinStats) == cMaxMinsToStore {
				st.ByMinStats = st.ByMinStats[1:]
			}
		}

		if len(st.BySecStats) == 60 {
			st.BySecStats = st.BySecStats[1:]
		}
		st.queries = 0
		st.inserts = 0

		if st.stop {
			return
		}
	}
}

func (mg *Manager) GroupInfoApiHandler(w http.ResponseWriter, r *http.Request) {
	userId := r.FormValue("uid")
	key := r.FormValue("key")
	groupID := r.FormValue("group")

	group, err := mg.shardsModel.GetGroupByUserKeyId(userId, key, groupID)
	if err != nil {
		// User not authorised to access to this shard
		w.WriteHeader(401)
		w.Write([]byte(fmt.Sprintf("%s", err)))

		return
	}

	response := make(map[string]*statsReqSec)
	if _, ok := mg.reqSecStats[groupID]; ok {
		mg.reqSecStats[groupID].RecTreeStatus = mg.acquiredShards[groupID].GetStatus()
		response[instances.GetHostName()] = mg.reqSecStats[groupID]
	}

	// If this is a direct call, visit all the remaining shards in order to
	// get the necessary info from them
	if r.FormValue("fw") == "" {
		for _, shard := range group.ShardsByAddr {
			if shard.Addr != instances.GetHostName() {
				vals := url.Values{
					"uid":   {userId},
					"key":   {key},
					"group": {groupID},
					"fw":    {"1"},
				}
				resp, err := http.PostForm(
					fmt.Sprintf("http://%s:%d%s", shard.Addr, mg.port, CGroupInfoPath),
					vals)

				if err != nil {
					log.Error("Can't retreive group information from instance:", shard.Addr, "Error:", err)
				} else {
					defer resp.Body.Close()
					remoteResp, err := ioutil.ReadAll(resp.Body)
					info := make(map[string]*statsReqSec)
					if err = json.Unmarshal([]byte(remoteResp), &info); err == nil {
						for k, v := range info {
							response[k] = v
						}
					} else {
						log.Error("Problem trying to get group information from host:", shard.Addr)
					}
				}
			}
		}
	}

	respJson, _ := json.Marshal(response)
	// User not authorised to access to this shard
	w.WriteHeader(200)
	w.Write(respJson)
}

func (mg *Manager) ScoresApiHandler(w http.ResponseWriter, r *http.Request) {
	userId := r.FormValue("uid")
	key := r.FormValue("key")
	groupID := r.FormValue("group")
	id := r.FormValue("id")
	elemScores := r.FormValue("scores")
	maxRecs := r.FormValue("max_recs")
	justAdd := r.FormValue("insert") != ""

	//log.Debug("New API request: uid:", userId, "key:", key, "groupID:", groupID, "id:", "elemScores:", elemScores, "maxRecs:", maxRecs, "justAdd:", justAdd)

	group, err := mg.shardsModel.GetGroupByUserKeyId(userId, key, groupID)
	if err != nil {
		// User not authorised to access to this shard
		w.WriteHeader(401)
		w.Write([]byte(fmt.Sprintf("%s", err)))

		return
	}

	rec, local := mg.acquiredShards[group.GroupID]
	if local && (rec.GetStatus() == recommender.STATUS_ACTIVE || rec.GetStatus() == recommender.STATUS_NORECORDS) {
		if _, ok := mg.reqSecStats[group.GroupID]; ok {
			mg.reqSecStats[group.GroupID].mutex.Lock()
			if justAdd {
				mg.reqSecStats[group.GroupID].inserts++
			} else {
				mg.reqSecStats[group.GroupID].queries++
			}
			mg.reqSecStats[group.GroupID].mutex.Unlock()
			if (!justAdd && mg.reqSecStats[group.GroupID].queries > group.MaxReqSec) ||
				(justAdd && mg.reqSecStats[group.GroupID].inserts > group.MaxInsertReqSec) {
				w.WriteHeader(429)
				w.Write([]byte("Too Many Requests"))

				return
			}
		} else {
			mg.reqSecStats[group.GroupID] = &statsReqSec{
				BySecStats: []uint64{},
				ByMinStats: []uint64{},
				queries:    0,
				inserts:    0,
			}
			go mg.reqSecStats[group.GroupID].monitorStats()
		}

		jsonScores := make(map[string]uint8)
		scores := make(map[uint64]uint8)
		err = json.Unmarshal([]byte(elemScores), &jsonScores)

		for k, v := range jsonScores {
			if elemId, err := strconv.ParseInt(k, 10, 64); err == nil {
				scores[uint64(elemId)] = v
			} else {
				w.WriteHeader(400)
				w.Write([]byte(fmt.Sprintf("Error: %s", err)))

				return
			}
		}

		if err != nil {
			// User not authorised to access to this shard
			w.WriteHeader(400)
			w.Write([]byte(fmt.Sprintf("Error: %s", err)))

			return
		}
		idInt, err := strconv.ParseInt(id, 10, 64)
		if err != nil {
			w.WriteHeader(500)
			w.Write([]byte("The specified value for the record \"id\" has to be an integer"))

			return
		}
		if justAdd {
			rec.AddRecord(uint64(idInt), scores)

			// User not authorised to access to this shard
			w.WriteHeader(200)
			w.Write([]byte(fmt.Sprintf(`{
				"success": true,
				"reqs_sec": %d
				"stored_elements": %d
			}`, mg.reqSecStats[group.GroupID].inserts, rec.GetTotalElements())))

			return
		} else {
			log.Debug("API Max records:", maxRecs)
			maxRecsInt, err := strconv.ParseInt(maxRecs, 10, 64)
			if err != nil {
				w.WriteHeader(500)
				w.Write([]byte("The specified value for the record \"max_recs\" has to be an integer"))

				return
			}
			recommendations := rec.CalcScores(uint64(idInt), scores, int(maxRecsInt))
			if len(recommendations) > 0 {
				result, _ := json.Marshal(recommendations)
				// User not authorised to access to this shard
				w.WriteHeader(200)
				w.Write([]byte(fmt.Sprintf(`{
					"success": true,
					"stored_elements": %d,
					"reqs_sec": %d,
					"recs": %s
				}`, rec.GetTotalElements(), mg.reqSecStats[group.GroupID].queries, string(result))))
			} else {
				w.WriteHeader(200)
				w.Write([]byte(fmt.Sprintf(`{
					"success": false,
					"status": "Adquiring data",
					"reqs_sec": %d,
					"stored_elements": %d,
					"recs": []
				}`, mg.reqSecStats[group.GroupID].queries, rec.GetTotalElements())))
			}

			return
		}
	} else {
		log.Debug("Remote API request", group, "Shards:", group.Shards)
		// TODO Get the results from another instance
		var shard *shardinfo.Shard
		var addr string

		hostsVisited := strings.Split(r.FormValue("hosts_visited"), ",")
		hostsVisited = append(hostsVisited, instances.GetHostName())

		visitedHostsMap := make(map[string]bool)
		for _, host := range hostsVisited {
			visitedHostsMap[host] = true
		}

		// Get a random instance with this shard
		for addr, shard = range group.ShardsByAddr {
			if _, visited := visitedHostsMap[addr]; !visited {
				break
			}
		}

		if shard == nil || addr == instances.GetHostName() {
			w.WriteHeader(503)
			w.Write([]byte("The server is provisioning the recomender system, the shard will be available soon, please be patient"))

			return
		}

		vals := url.Values{
			"uid":           {userId},
			"key":           {key},
			"group":         {groupID},
			"id":            {id},
			"scores":        {elemScores},
			"hosts_visited": {strings.Join(hostsVisited, ",")},
		}
		if len(maxRecs) > 0 {
			vals.Add("max_recs", maxRecs)
		}
		if justAdd {
			vals.Add("insert", "true")
		}

		resp, err := http.PostForm(
			fmt.Sprintf("http://%s:%d%s", shard.Addr, mg.port, CRecPath),
			vals)

		if err != nil {
			w.WriteHeader(500)

			return
		} else {
			defer resp.Body.Close()
			responseBody, err := ioutil.ReadAll(resp.Body)
			if err != nil {
				w.WriteHeader(500)
				w.Write([]byte("Internal server error"))

				return
			}
			w.WriteHeader(resp.StatusCode)
			w.Write(responseBody)

			log.Debug("API result:", string(responseBody))
		}
	}
}

func (mg *Manager) canAcquireNewShard(group *shardinfo.GroupInfo) bool {
	maxShardsToAdquire := mg.instancesModel.GetMaxShardsToAdquire(mg.shardsModel.GetTotalNumberOfShards())
	if maxShardsToAdquire <= len(mg.acquiredShards) {
		return false
	}

	totalElems := uint64(0)
	for _, groups := range mg.shardsModel.GetAllGroups() {
		for _, groupMem := range groups {
			totalElems += groupMem.MaxElements
		}
	}
	allocableElems := cfg.GetInt("mem", "instance-mem-gb") * cfg.GetInt("mem", "records-by-gb")

	log.Debug("Max elems to alloc:", allocableElems, "Current elements:", totalElems, "Group Elements:", group.MaxElements)

	return uint64(allocableElems) >= totalElems+group.MaxElements
}

func (mg *Manager) manage() {
	go mg.recalculateRecs()

	for mg.active {
		for _, groups := range mg.shardsModel.GetAllGroups() {
			for _, group := range groups {
				if mg.canAcquireNewShard(group) {
					if acquired, err := group.AcquireShard(); acquired && err == nil {
						mg.acquiredShard(group)
					}
				}
			}
		}

		time.Sleep(time.Second)
	}

	mg.shardsModel.ReleaseAllAcquiredShards()
	mg.finished = true
}

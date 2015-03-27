package shardsmanager

import (
	"encoding/json"
	"fmt"
	"github.com/alonsovidales/pit/log"
	"github.com/alonsovidales/pit/models/instances"
	"github.com/alonsovidales/pit/models/shard_info"
	"github.com/alonsovidales/pit/recommender"
	"io/ioutil"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

const (
	cRecPath        = "/get_rec"
	cCheckHealtyURI = "/check_healty"
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
	bySecStats []uint64
	byMinStats []uint64
	queries    uint64
	inserts    uint64
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
}

func (mg *Manager) recalculateRecs() {
	for {
		for _, rec := range mg.acquiredShards {
			rec.RecalculateTree()
			rec.SaveBackup()
		}

		time.Sleep(time.Second * 30)
	}
}

func (st *statsReqSec) monitorStats() {
	c := time.Tick(time.Second)
	i := 0
	for range c {
		st.bySecStats[i] = st.queries
		i++
		if i == 60 {
			i = 0
			v := uint64(0)
			for _, q := range st.bySecStats {
				v += q
			}
			st.byMinStats = append(st.byMinStats, v)
		}
		st.queries = 0
		st.inserts = 0
	}
}

func (mg *Manager) scoresApiHandler(w http.ResponseWriter, r *http.Request) {
	userId := r.FormValue("uid")
	key := r.FormValue("key")
	groupId := r.FormValue("group")
	id := r.FormValue("id")
	elemScores := r.FormValue("scores")
	maxRecs := r.FormValue("max_recs")
	justAdd := r.FormValue("insert") != ""

	//log.Debug("New API request: uid:", userId, "key:", key, "groupId:", groupId, "id:", "elemScores:", elemScores, "maxRecs:", maxRecs, "justAdd:", justAdd)

	group, err := mg.shardsModel.GetGroupByUserKeyId(userId, key, groupId)
	if err != nil {
		// User not authorised to access to this shard
		w.WriteHeader(401)
		w.Write([]byte(fmt.Sprintf("%s", err)))

		return
	}

	if rec, local := mg.acquiredShards[group.GroupID]; local && rec.GetStatus() == recommender.STATUS_ACTIVE {
		if _, ok := mg.reqSecStats[group.GroupID]; ok {
			if justAdd {
				mg.reqSecStats[group.GroupID].inserts++
			} else {
				mg.reqSecStats[group.GroupID].queries++
			}
			if (!justAdd && mg.reqSecStats[group.GroupID].queries > group.MaxReqSec) ||
				(justAdd && mg.reqSecStats[group.GroupID].inserts > group.MaxInsertReqSec) {
				w.WriteHeader(429)
				w.Write([]byte("Too Many Requests"))

				return
			}
		} else {
			mg.reqSecStats[group.GroupID] = &statsReqSec{
				bySecStats: make([]uint64, 60),
				byMinStats: []uint64{},
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
		// Get a random instance with this shard
		for addr, shard = range group.ShardsByAddr {
			if addr != instances.GetHostName() {
				break
			}
		}

		if shard == nil || addr == instances.GetHostName() {
			w.WriteHeader(503)
			w.Write([]byte("The server is provisioning the recomender system, the shard will be available soon, please be patient"))

			return
		}

		vals := url.Values{
			"uid":    {userId},
			"key":    {key},
			"group":  {groupId},
			"id":     {id},
			"scores": {elemScores},
		}
		if len(maxRecs) > 0 {
			vals.Add("max_recs", maxRecs)
		}
		if justAdd {
			vals.Add("insert", "true")
		}

		resp, err := http.PostForm(
			fmt.Sprintf("http://%s:%d%s", shard.Addr, mg.port, cRecPath),
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

func (mg *Manager) startApi() {
	mux := http.NewServeMux()
	mux.HandleFunc(cCheckHealtyURI, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(200)
		w.Write([]byte("OK"))
	})
	mux.HandleFunc(cRecPath, mg.scoresApiHandler)
	log.Info("Starting API server on port:", mg.port)
	go http.ListenAndServe(fmt.Sprintf(":%d", mg.port), mux)
}

func (mg *Manager) manage() {
	go mg.recalculateRecs()
	go mg.startApi()

	for mg.active {
		maxShardsToAdquire := mg.instancesModel.GetMaxShardsToAdquire(mg.shardsModel.GetTotalNumberOfShards())
		if maxShardsToAdquire > len(mg.acquiredShards) {
			for _, groups := range mg.shardsModel.GetAllGroups() {
				for _, group := range groups {
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

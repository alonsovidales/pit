package shardsmanager

import (
	"encoding/json"
	"fmt"
	"github.com/alonsovidales/pit/cfg"
	"github.com/alonsovidales/pit/log"
	"github.com/alonsovidales/pit/models/instances"
	"github.com/alonsovidales/pit/models/shard_info"
	"github.com/alonsovidales/pit/models/users"
	"github.com/alonsovidales/pit/recommender"
	"github.com/nu7hatch/gouuid"
	"io/ioutil"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	CScoresPath    = "/scores"
	CRecPath       = "/rec"
	CGroupInfoPath = "/info"

	// User required actions
	CRegenerateGroupKey  = "/generate_group_key"
	CGetGroupsByUser     = "/get_groups_by_user"
	CAddUpdateGroup      = "/add_group"
	CSetShardsGroup      = "/set_shards_group"
	CRemoveShardsContent = "/remove_group_shards_content"

	cMaxMinsToStore = 1440 // A day
)

type Manager struct {
	awsRegion      string
	s3BackupsPath  string
	port           int
	active         bool
	finished       bool
	acquiredShards map[string]recommender.Int

	shardsModel    shardinfo.ModelInt
	instancesModel instances.ModelInt
	reqSecStats    map[string]*statsReqSec
	usersModel     users.ModelInt
}

type statsReqSec struct {
	StoredElements uint64   `json:"stored_elements"`
	RecTreeStatus  string   `json:"rec_tree_status"`
	BySecStats     []uint64 `json:"queries_by_sec"`
	ByMinStats     []uint64 `json:"queries_by_min"`
	queries        uint64
	inserts        uint64
	mutex          sync.Mutex
	stop           bool
}

func Init(prefix, awsRegion, s3BackupsPath string, port int, usersModel users.ModelInt, adminEmail string) (mg *Manager) {
	mg = &Manager{
		s3BackupsPath: s3BackupsPath,
		port:          port,
		active:        true,
		finished:      false,
		reqSecStats:   make(map[string]*statsReqSec),

		shardsModel:    shardinfo.GetModel(prefix, awsRegion, adminEmail),
		instancesModel: instances.InitAndKeepAlive(prefix, awsRegion, true),
		awsRegion:      awsRegion,
		acquiredShards: make(map[string]recommender.Int),
		usersModel:     usersModel,
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
	mg.reqSecStats[group.GroupID] = &statsReqSec{
		BySecStats: []uint64{},
		ByMinStats: []uint64{},
		queries:    0,
		inserts:    0,
	}
	go mg.reqSecStats[group.GroupID].monitorStats()
	mg.acquiredShards[group.GroupID] = rec

	go mg.keepUpdateGroup(group.GroupID)
	log.Info("Finished acquisition of shard on group:", group.GroupID)
}

func (mg *Manager) keepUpdateGroup(groupID string) {
	for {
		gr := mg.shardsModel.GetGroupByID(groupID)
		if gr == nil || !gr.IsThisInstanceOwner() {
			mg.acquiredShards[groupID].Stop()
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
	for _ = range c {
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

		if len(st.BySecStats) == 61 {
			st.BySecStats = st.BySecStats[1:]
		}
		st.queries = 0
		st.inserts = 0

		if st.stop {
			return
		}
	}
}

func (mg *Manager) RemoveShardsContent(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")

	userID := r.FormValue("u")
	key := r.FormValue("uk")
	shardKey := r.FormValue("k")
	groupID := r.FormValue("g")

	user := mg.usersModel.GetUserInfo(userID, key)
	if user == nil {
		w.WriteHeader(401)
		w.Write([]byte("Unauthorized"))
		return
	}

	group, err := mg.shardsModel.GetGroupByUserKeyID(userID, shardKey, groupID)
	if err != nil {
		// User not authorised to access to this shard
		w.WriteHeader(401)
		w.Write([]byte(fmt.Sprintf("%s", err)))

		return
	}
	result := group.RemoveAllContent(
		recommender.NewShard(mg.s3BackupsPath, group.GroupID, group.MaxElements, group.MaxScore, mg.awsRegion),
	)
	if !result {
		w.WriteHeader(500)
		w.Write([]byte("KO"))
	}
	user.AddActivityLog(users.CActivityShardsType, fmt.Sprintf("Removed all the shards content for group: %s", groupID), r.RemoteAddr)
	w.WriteHeader(200)
	w.Write([]byte("OK"))
}

func (mg *Manager) GroupInfoAPIHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")

	userID := r.FormValue("uid")
	key := r.FormValue("key")
	groupID := r.FormValue("group")

	group, err := mg.shardsModel.GetGroupByUserKeyID(userID, key, groupID)
	if err != nil {
		// User not authorised to access to this shard
		w.WriteHeader(401)
		w.Write([]byte(fmt.Sprintf("%s", err)))

		return
	}

	response := make(map[string]*statsReqSec)
	if _, ok := mg.reqSecStats[groupID]; ok {
		mg.reqSecStats[groupID].RecTreeStatus = mg.acquiredShards[groupID].GetStatus()
		mg.reqSecStats[groupID].StoredElements = mg.acquiredShards[groupID].GetStoredElements()
		response[instances.GetHostName()] = mg.reqSecStats[groupID]
	}

	// If this is a direct call, visit all the remaining shards in order to
	// get the necessary info from them
	if r.FormValue("fw") == "" {
		for _, shard := range group.ShardsByAddr {
			if shard.Addr != instances.GetHostName() {
				vals := url.Values{
					"uid":   {userID},
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

	respJSON, _ := json.Marshal(response)
	// User not authorised to access to this shard
	w.WriteHeader(200)
	w.Write(respJSON)
}

func (mg *Manager) AddUpdateGroup(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")

	var reqs, records uint64

	uid := r.FormValue("u")
	uKey := r.FormValue("uk")
	guid := r.FormValue("guid")
	user := mg.usersModel.GetUserInfo(uid, uKey)
	if user == nil {
		w.WriteHeader(401)
		w.Write([]byte("Unauthorized"))
		return
	}

	groupType := r.FormValue("gt")
	switch groupType {
	case "s":
		reqs = cfg.GetUint64("group-types", "small-reqs")
		records = cfg.GetUint64("group-types", "small-records")
	case "m":
		reqs = cfg.GetUint64("group-types", "medium-reqs")
		records = cfg.GetUint64("group-types", "medium-records")
	case "l":
		reqs = cfg.GetUint64("group-types", "large-reqs")
		records = cfg.GetUint64("group-types", "large-records")
	case "xl":
		reqs = cfg.GetUint64("group-types", "x-large-reqs")
		records = cfg.GetUint64("group-types", "x-large-records")
	case "xxl":
		reqs = cfg.GetUint64("group-types", "xx-large-reqs")
		records = cfg.GetUint64("group-types", "xx-large-records")
	default:
		w.WriteHeader(422)
		w.Write([]byte("Group type required"))
		return
	}

	shardsStr := r.FormValue("shards")
	shards, err := strconv.ParseInt(shardsStr, 10, 64)
	if err != nil {
		w.WriteHeader(422)
		w.Write([]byte("The param shards is not an integer"))
		return
	}
	maxScoreStr := r.FormValue("max-score")
	maxScore, err := strconv.ParseInt(maxScoreStr, 10, 64)
	if err != nil {
		w.WriteHeader(422)
		w.Write([]byte("The param max-score is not an integer"))
		return
	}

	uuid, _ := uuid.NewV4()
	guid = guid + ":" + uuid.String()
	_, key, err := mg.shardsModel.AddUpdateGroup(uid, guid, int(shards), records, reqs, reqs*4, uint8(maxScore))
	if err != nil {
		w.WriteHeader(500)
		w.Write([]byte(fmt.Sprintf("Error trying to add a new group:", err)))
		return
	}

	user.AddActivityLog(users.CActivityShardsType, fmt.Sprintf("Added new group of type:", groupType, "with Shards:", shards, "GUID:", guid), r.RemoteAddr)
	w.WriteHeader(200)
	w.Write([]byte(fmt.Sprintf(`{"success": true, "key": "%s"}`, key)))
}

func (mg *Manager) RegenerateGroupKey(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")

	uid := r.FormValue("u")
	uKey := r.FormValue("uk")
	gid := r.FormValue("g")
	key := r.FormValue("k")

	user := mg.usersModel.GetUserInfo(uid, uKey)
	if user == nil {
		w.WriteHeader(401)
		w.Write([]byte("Unauthorized"))
		return
	}

	if group, err := mg.shardsModel.GetGroupByUserKeyID(uid, key, gid); err == nil {
		if key, err := group.RegenerateKey(); err == nil {
			user.AddActivityLog(users.CActivityShardsType, "Regenerated group key", r.RemoteAddr)
			w.WriteHeader(200)
			w.Write([]byte(key))
		} else {
			w.WriteHeader(500)
			w.Write([]byte("Problem re-generating key"))
		}
	} else {
		w.WriteHeader(401)
		w.Write([]byte("Unauthorized"))
	}
}

func (mg *Manager) GetGroupsByUser(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")

	uid := r.FormValue("u")
	uKey := r.FormValue("uk")
	user := mg.usersModel.GetUserInfo(uid, uKey)
	if user == nil {
		w.WriteHeader(401)
		w.Write([]byte("Unauthorized"))
		return
	}

	groups := mg.shardsModel.GetAllGroupsByUserID(uid)
	groupsJSON, _ := json.Marshal(groups)
	w.WriteHeader(200)
	w.Write(groupsJSON)
}

func (mg *Manager) DelGroup(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")

	uid := r.FormValue("u")
	uKey := r.FormValue("uk")
	gid := r.FormValue("g")
	key := r.FormValue("k")

	user := mg.usersModel.GetUserInfo(uid, uKey)
	if user == nil {
		w.WriteHeader(401)
		w.Write([]byte("Unauthorized"))
		return
	}

	if _, err := mg.shardsModel.GetGroupByUserKeyID(uid, key, gid); err == nil {
		if err := mg.shardsModel.RemoveGroup(gid); err != nil {
			w.WriteHeader(500)
			w.Write([]byte("Internal Server Error"))
			return
		}
		user.AddActivityLog(users.CActivityShardsType, fmt.Sprintf("Removed group:", gid), r.RemoteAddr)
		w.WriteHeader(200)
		w.Write([]byte("OK"))
	} else {
		w.WriteHeader(401)
		w.Write([]byte("Unauthorized"))
	}
}

func (mg *Manager) SetShards(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")

	uid := r.FormValue("u")
	uKey := r.FormValue("uk")
	gid := r.FormValue("g")
	key := r.FormValue("k")

	user := mg.usersModel.GetUserInfo(uid, uKey)
	if user == nil {
		w.WriteHeader(401)
		w.Write([]byte("Unauthorized"))
		return
	}

	if group, err := mg.shardsModel.GetGroupByUserKeyID(uid, key, gid); err == nil {
		shards, err := strconv.ParseInt(r.FormValue("s"), 10, 64)
		if err != nil {
			w.WriteHeader(422)
			w.Write([]byte("The number of shards has to be an integer"))
			return
		}

		if err := group.SetNumShards(int(shards)); err != nil {
			log.Error("Problem trying to store a new number of shards, Error:", err)
			w.WriteHeader(500)
			w.Write([]byte("Internal Server Error"))
			return
		}

		user.AddActivityLog(
			users.CActivityShardsType,
			fmt.Sprintf("Modified number of shards on group: %s, to: %d", gid, shards),
			r.RemoteAddr)
		w.WriteHeader(200)
		w.Write([]byte("OK"))
	} else {
		w.WriteHeader(401)
		w.Write([]byte("Unauthorized"))
		return
	}
}

func (mg *Manager) ScoresAPIHandler(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")

	userID := r.FormValue("uid")
	key := r.FormValue("key")
	groupID := r.FormValue("group")

	group, err := mg.shardsModel.GetGroupByUserKeyID(userID, key, groupID)
	if err != nil {
		// User not authorised to access to this shard
		w.WriteHeader(401)
		w.Write([]byte(fmt.Sprintf("%s", err)))

		return
	}

	id := r.FormValue("id")
	elemScores := r.FormValue("scores")
	items := r.FormValue("items")
	maxRecs := r.FormValue("max_recs")
	justAdd := r.FormValue("insert") != ""

	rec, local := mg.acquiredShards[group.GroupID]
	if local && (rec.GetStatus() == recommender.StatusActive || rec.GetStatus() == recommender.StatusNoRecords) {
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

		if r.URL.Path == CScoresPath {
			// This is a query for average scores for the elements
			itemsSlice := []uint64{}
			if err = json.Unmarshal([]byte(items), &itemsSlice); err != nil {
				w.WriteHeader(400)
				w.Write([]byte(fmt.Sprintf("Error: %s", err)))
			}

			scores := rec.GetAvgScores(itemsSlice)
			scoresToJson := make(map[string]float64)
			for k, v := range scores {
				scoresToJson[fmt.Sprintf("%d", k)] = v
			}

			result, _ := json.Marshal(scoresToJson)
			// User not authorised to access to this shard
			w.WriteHeader(200)
			w.Write([]byte(fmt.Sprintf(`{
				"success": true,
				"reqs_sec": %d,
				"stored_elements": %d,
				"scores": %s
			}`, mg.reqSecStats[group.GroupID].inserts, rec.GetStoredElements(), string(result))))

			return
		}

		// This is a query for recommendations
		jsonScores := make(map[string]uint8)
		scores := make(map[uint64]uint8)

		if err = json.Unmarshal([]byte(elemScores), &jsonScores); err != nil {
			w.WriteHeader(400)
			w.Write([]byte(fmt.Sprintf("Error: %s", err)))

			return
		}
		for k, v := range jsonScores {
			if elemID, err := strconv.ParseInt(k, 10, 64); err == nil {
				scores[uint64(elemID)] = v
			} else {
				w.WriteHeader(400)
				w.Write([]byte(fmt.Sprintf("Error: %s", err)))

				return
			}
		}

		idInt, err := strconv.ParseInt(id, 10, 64)
		if err != nil {
			w.WriteHeader(400)
			w.Write([]byte("The specified value for the record \"id\" has to be an integer"))

			return
		}

		if justAdd {
			rec.AddRecord(uint64(idInt), scores)

			// User not authorised to access to this shard
			w.WriteHeader(200)
			w.Write([]byte(fmt.Sprintf(`{
				"success": true,
				"reqs_sec": %d,
				"stored_elements": %d
			}`, mg.reqSecStats[group.GroupID].inserts, rec.GetStoredElements())))

			return
		}

		maxRecsInt, err := strconv.ParseInt(maxRecs, 10, 64)
		if err != nil {
			w.WriteHeader(400)
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
			}`, rec.GetStoredElements(), mg.reqSecStats[group.GroupID].queries, string(result))))
		} else {
			w.WriteHeader(200)
			w.Write([]byte(fmt.Sprintf(`{
				"success": false,
				"status": "Adquiring data",
				"reqs_sec": %d,
				"stored_elements": %d,
				"recs": []
			}`, mg.reqSecStats[group.GroupID].queries, rec.GetStoredElements())))
		}

		return
	}

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
		"uid":           {userID},
		"key":           {key},
		"group":         {groupID},
		"id":            {id},
		"scores":        {elemScores},
		"items":         {items},
		"hosts_visited": {strings.Join(hostsVisited, ",")},
	}
	if len(maxRecs) > 0 {
		vals.Add("max_recs", maxRecs)
	}
	if justAdd {
		vals.Add("insert", "true")
	}

	resp, err := http.PostForm(
		fmt.Sprintf("http://%s:%d%s", shard.Addr, mg.port, r.URL.Path),
		vals)

	if err != nil {
		w.WriteHeader(500)

		return
	}
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

func (mg *Manager) canAcquireNewShard(group *shardinfo.GroupInfo) bool {
	maxShardsToAdquire := mg.instancesModel.GetMaxShardsToAdquire(mg.shardsModel.GetTotalNumberOfShards())
	if maxShardsToAdquire <= len(mg.acquiredShards) {
		return false
	}

	totalElems := uint64(0)
	for _, group := range mg.acquiredShards {
		totalElems += group.GetTotalElements()
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

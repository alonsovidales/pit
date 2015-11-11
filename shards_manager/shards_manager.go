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
	"reflect"
	"strconv"
	"strings"
	"sync"
	"time"
)

const (
	// CScoresPath Endpoint that will provide the scores for some provided
	// items
	CScoresPath = "/scores"
	// CRecPath Path that will provide the recommendations based on a list
	// of items
	CRecPath = "/rec"
	// CGroupInfoPath Endpoint that returns information from all the shards
	// that composes the group, status, elements stored, etc
	CGroupInfoPath = "/info"

	// User required actions

	// CDelGroup Removes a shard and it contents
	CDelGroup = "/del_group"
	// CRegenerateGroupKey Endpoint used to regenerate the security key for
	// a shard
	CRegenerateGroupKey = "/generate_group_key"
	// CGetGroupsByUser Endpoint that will return the list of groups for a
	// specifig user
	CGetGroupsByUser = "/get_groups_by_user"
	// CAddUpdateGroup Endpoint to add a new group, or update the
	// information for a existing one
	CAddUpdateGroup = "/add_group"
	// CSetShardsGroup Sets the number of shards by a group
	CSetShardsGroup = "/set_shards_group"
	// CRemoveShardsContent Enpoint that removes all the content form the
	// persistence layer and cleans up the data on the persistence storage
	CRemoveShardsContent = "/remove_group_shards_content"

	// cMaxMinsToStore Max time in minutes to keep the metrics in memory
	cMaxMinsToStore = 1440 // A day
)

// Manager Structure that provides HTTP access to manage all the different
// groups and shards on each grorup
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

// statsReqSec Statistics for a shard
type statsReqSec struct {
	// StoredElements Number of stored elements on this shard
	StoredElements uint64 `json:"stored_elements"`
	// RecTreeStatus Current status of the recommender tree
	RecTreeStatus string `json:"rec_tree_status"`
	// BySecStats Number of queries per second
	BySecStats []uint64 `json:"queries_by_sec"`
	// ByMinStats Number of queries per minute
	ByMinStats []uint64 `json:"queries_by_min"`
	queries    uint64
	inserts    uint64
	mutex      sync.Mutex
	stop       bool
}

// Init Initializes and returns the Manager for a group, this method also
// launches the monitorization process in background
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

// Stop deactivates a group, and stops all the management tasks
func (mg *Manager) Stop() {
	mg.active = false
}

// IsFinished Returs if the manager has finish the adquisition of shards for
// this group or not
func (mg *Manager) IsFinished() bool {
	return mg.finished
}

// acquiredShard After determine that is possible to acquire a shard on this
// local machine, this method is requested to set up the shard and all the
// monitorizaion processes
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

	go mg.keepUpdateGroup(group.GetUserID(), group.GroupID)
	log.Info("Finished acquisition of shard on group:", group.GroupID)
}

// recalculateBillingForUser Recalculates a bill for the given user based in
// the used shards and time for each type
func (mg *Manager) recalculateBillingForUser(userID string) {
	groups := mg.shardsModel.GetAllGroupsByUserID(userID)
	us := mg.usersModel.AdminGetUserInfoByID(userID)
	if us == nil {
		log.Error("User:", userID, "not found")
		return
	}
	shardsInUseByGroupAndType := make(map[string]int)
	for _, gr := range groups {
		shardsInUseByGroupAndType[fmt.Sprintf("%s:%s", gr.Type, gr.GroupID)] = len(gr.ShardsByAddr)
	}

	lastBillInfo := us.GetLastBillInfo()
	if lastBillInfo == nil || (!reflect.DeepEqual(shardsInUseByGroupAndType, lastBillInfo.Inst) && (lastBillInfo.Ts+5 < time.Now().Unix())) {
		us.AddBillingHist(shardsInUseByGroupAndType)
	}
}

// keepUpdateGroup updates each second the status of the shard on S3 and keeps
// it adquired for the local machine
func (mg *Manager) keepUpdateGroup(uid, groupID string) {
	for {
		gr := mg.shardsModel.GetGroupByID(groupID)
		if gr == nil || !gr.IsThisInstanceOwner() {
			mg.acquiredShards[groupID].Stop()
			delete(mg.acquiredShards, groupID)
			mg.reqSecStats[groupID].stop = true
			delete(mg.reqSecStats, groupID)
			log.Info("Shard released on group:", groupID)
			mg.recalculateBillingForUser(uid)

			return
		}

		mg.acquiredShards[groupID].SetMaxElements(gr.MaxElements)
		mg.acquiredShards[groupID].SetMaxScore(gr.MaxScore)

		time.Sleep(time.Second)
	}
}

// recalculateRecs Determines is the shard have receive any new data each 30
// seconds, and in case of have new data launched the reprocesed of the tree
// and stores the backup after finish
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

// RemoveShardsContent Used to wipe the content of a shard included the content on the persistance layer
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

// GroupInfoAPIHandler Returns all the information relative to a group of shards
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

// AddUpdateGroup Creates a new group of shards, or in case of exists updates
// an existing group
func (mg *Manager) AddUpdateGroup(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", "*")

	uid := r.FormValue("u")
	uKey := r.FormValue("uk")
	guid := r.FormValue("guid")

	// Sanitize the group ID
	guid = strings.Replace(guid, " ", "-", -1)
	guid = strings.Replace(guid, "<", "", -1)
	guid = strings.Replace(guid, ">", "", -1)
	guid = strings.Replace(guid, "\"", "", -1)
	guid = strings.Replace(guid, "'", "", -1)

	user := mg.usersModel.GetUserInfo(uid, uKey)
	if user == nil {
		w.WriteHeader(401)
		w.Write([]byte("Unauthorized"))
		return
	}

	groupType := r.FormValue("gt")
	reqs, records, _ := users.GetGroupInfo(groupType)
	if reqs == 0 {
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
	maxScoreStr := r.FormValue("maxscore")
	maxScore, err := strconv.ParseInt(maxScoreStr, 10, 64)
	if err != nil {
		w.WriteHeader(422)
		w.Write([]byte("The param max-score is not an integer"))
		return
	}

	uuid, _ := uuid.NewV4()
	guid = guid + ":" + uuid.String()
	_, key, err := mg.shardsModel.AddUpdateGroup(groupType, uid, guid, int(shards), records, reqs, reqs*4, uint8(maxScore))
	if err != nil {
		w.WriteHeader(500)
		w.Write([]byte(fmt.Sprintf("Error trying to add a new group:", err)))
		return
	}

	user.AddActivityLog(
		users.CActivityShardsType,
		fmt.Sprintf("Added new group of type: %s with Shards: %d GUID: %s", groupType, shards, guid),
		r.RemoteAddr)
	mg.recalculateBillingForUser(uid)

	w.WriteHeader(200)
	w.Write([]byte(fmt.Sprintf(`{"success": true, "key": "%s"}`, key)))
}

// RegenerateGroupKey Creates a new random key for a group
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

// GetGroupsByUser Returns the group registered for a user
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

// DelGroup removes a group of shards and all the content on the shards
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
		user.AddActivityLog(users.CActivityShardsType, fmt.Sprintf("Removed group: %s", gid), r.RemoteAddr)
		go func() {
			time.Sleep(10)
			mg.recalculateBillingForUser(uid)
		}()

		w.WriteHeader(200)
		w.Write([]byte("OK"))
	} else {
		w.WriteHeader(401)
		w.Write([]byte("Unauthorized"))
	}
}

// SetShards Updates the number of shards that composes a group
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
		mg.recalculateBillingForUser(uid)

		w.WriteHeader(200)
		w.Write([]byte("OK"))
	} else {
		w.WriteHeader(401)
		w.Write([]byte("Unauthorized"))
		return
	}
}

// ScoresAPIHandler Returns the scores for a group of items on a shard, in case
// of can't find a shard available on the local machine, this method propagates
// the query to another instance
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
			scoresToJSON := make(map[string]float64)
			for k, v := range scores {
				scoresToJSON[fmt.Sprintf("%d", k)] = v
			}

			result, _ := json.Marshal(scoresToJSON)
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

// canAcquireNewShard Checks if this machine have enough resources to allocate
// a shard of the given group
func (mg *Manager) canAcquireNewShard(group *shardinfo.GroupInfo) bool {
	maxShardsToAcquire := mg.instancesModel.GetMaxShardsToAcquire(mg.shardsModel.GetTotalNumberOfShards())
	if maxShardsToAcquire <= len(mg.acquiredShards) {
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

// manage mintorize the status of the shards, updates bills, etc
func (mg *Manager) manage() {
	go mg.recalculateRecs()

	for mg.active {
		users := make(map[string]bool)
		for _, groups := range mg.shardsModel.GetAllGroups() {
			for _, group := range groups {
				users[group.UserID] = true
				if mg.canAcquireNewShard(group) {
					if acquired, err := group.AcquireShard(); acquired && err == nil {
						mg.acquiredShard(group)
					}
				}
			}
		}

		for usID := range users {
			mg.recalculateBillingForUser(usID)
		}

		time.Sleep(time.Second)
	}

	mg.shardsModel.ReleaseAllAcquiredShards()
	mg.finished = true
}

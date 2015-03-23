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

const cRecPath = "/get_rec"
const cCheckHealtyURI = "/check_healty"

type Manager struct {
	awsRegion      string
	s3BackupsPath  string
	port           int
	active         bool
	finished       bool
	adquiredShards map[string]recommender.RecommenderInt

	shardsModel    shardinfo.ModelInt
	instancesModel instances.InstancesModelInt
}

func Init(prefix, awsRegion, s3BackupsPath string, port int) (mg *Manager) {
	mg = &Manager{
		s3BackupsPath: s3BackupsPath,
		port:          port,
		active:        true,
		finished:      false,

		shardsModel:    shardinfo.GetModel(prefix, awsRegion),
		instancesModel: instances.InitAndKeepAlive(prefix, awsRegion, true),
		awsRegion:      awsRegion,
		adquiredShards:      make(map[string]recommender.RecommenderInt),
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

func (mg *Manager) adquiredShard(group *shardinfo.GroupInfo) {
	rec := recommender.NewShard(mg.s3BackupsPath, group.GroupId, group.MaxElements, group.MaxScore, mg.awsRegion)
	rec.LoadBackup()
	rec.RecalculateTree()
	mg.adquiredShards[group.GroupId] = rec
}

func (mg *Manager) recalculateRecs() {
	for {
		for _, rec := range mg.adquiredShards {
			rec.RecalculateTree()
			rec.SaveBackup()
		}

		time.Sleep(time.Second * 30)
	}
}

func (mg *Manager) scoresApiHandler(w http.ResponseWriter, r *http.Request) {
	userId := r.FormValue("uid")
	key := r.FormValue("key")
	groupId := r.FormValue("group")
	id := r.FormValue("id")
	elemScores := r.FormValue("scores")
	maxRecs := r.FormValue("max_recs")
	justAdd := r.FormValue("just_add")

	//log.Debug("New API request: uid:", userId, "key:", key, "groupId:", groupId, "id:", "elemScores:", elemScores, "maxRecs:", maxRecs, "justAdd:", justAdd)

	group, err := mg.shardsModel.GetGroupByUserKeyId(userId, key, groupId)
	if err != nil {
		// User not authorised to access to this shard
		w.WriteHeader(401)
		w.Write([]byte(fmt.Sprintf("%s", err)))

		return
	}

	if recommender, local := mg.adquiredShards[group.GroupId]; local {
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
		if len(justAdd) > 0 {
			recommender.AddRecord(uint64(idInt), scores)

			// User not authorised to access to this shard
			w.WriteHeader(200)
			w.Write([]byte(fmt.Sprintf(`{
				"success": true,
				"stored_elements": %d
			}`, recommender.GetTotalElements())))

			return
		} else {
			log.Debug("API Max records:", maxRecs)
			maxRecsInt, err := strconv.ParseInt(maxRecs, 10, 64)
			if err != nil {
				w.WriteHeader(500)
				w.Write([]byte("The specified value for the record \"max_recs\" has to be an integer"))

				return
			}
			recommendations := recommender.CalcScores(uint64(idInt), scores, int(maxRecsInt))
			if len(recommendations) > 0 {
				result, _ := json.Marshal(recommendations)
				// User not authorised to access to this shard
				w.WriteHeader(200)
				w.Write([]byte(fmt.Sprintf(`{
					"success": true,
					"stored_elements": %d,
					"recs": %s
				}`, recommender.GetTotalElements(), string(result))))
			} else {
				w.WriteHeader(200)
				w.Write([]byte(fmt.Sprintf(`{
					"success": false,
					"status": "Adquiring data",
					"stored_elements": %d,
					"recs": []
				}`, recommender.GetTotalElements())))
			}

			return
		}
	} else {
		log.Debug("Remote API request", group, "Shards:", group.Shards)
		// TODO Get the results from another instance
		var shard *shardinfo.Shard
		// Get a random instance with this shard
		for _, shard = range group.Shards {
			break
		}

		if shard.Addr == instances.GetHostName() {
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
		if len(justAdd) > 0 {
			vals.Add("just_add", justAdd)
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
		if maxShardsToAdquire > len(mg.adquiredShards) {
			for _, groups := range mg.shardsModel.GetAllGroups() {
				for _, group := range groups {
					if adquired, err := group.AcquireShard(); adquired && err == nil {
						mg.adquiredShard(group)
					}
				}
			}
		}

		time.Sleep(time.Second)
	}

	mg.shardsModel.ReleaseAllAdquiredShards()
	mg.finished = true
}

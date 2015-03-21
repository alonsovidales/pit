package shardinfo

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/alonsovidales/pit/log"
	"github.com/alonsovidales/pit/models/instances"
	"github.com/goamz/goamz/aws"
	"github.com/goamz/goamz/dynamodb"
	"sync"
	"time"
)

const (
	cTable             = "rec_shards"
	cPrimKey           = "groupId"
	cDefaultWRCapacity = 10

	cMaxHoursToStore   = 168 // A week
	cUpdatePeriod      = 5
	cUpdateShardPeriod = 2
	cShardTTL          = 10
)

var CErrGroupUserNotFound = errors.New("User not found")
var CErrGroupInUse = errors.New("Group ID in use")
var CErrGroupNotFound = errors.New("Group not found")
var CErrAuth = errors.New("Authentication problem")
var CErrMaxShardsByGroup = errors.New("Max number of shards for this group reached")
var CSharPrevOwnedGroup = errors.New("This instance yet owns a shard on this group")

type GroupInfoInt interface {
	AcquireShard() (adquired bool, err error)
	ReleaseShard()
}

type Shard struct {
	Addr   string `json:"addr"`
	LastTs uint32 `json:"last_ts"`

	ReqHour []uint64 `json:"reqs_hour"`
	ReqMin  []uint64 `json:"reqs_min"`
	ReqSec  uint64   `json:"reqs_sec"`
}

type GroupInfo struct {
	GroupInfoInt `json:"-"`

	UserId  string `json:"user_id"`
	Secret  string `json:"secret"`
	GroupId string `json:"group_id"`

	// MaxScore The max score for the group elements
	MaxScore uint8 `json:"tot_shards"`
	// NumShards Total number of shards by this group
	NumShards int `json:"tot_shards"`
	// MaxElements Total number of elements allocated by shard
	MaxElements uint64 `json:"max_elems"`
	// MaxReqSec Max number of requests / sec by shard
	MaxReqSec uint64 `json:"max_req_sec"`
	// MaxInsertReqSec Max number of insert requests by shard
	MaxInsertReqSec uint64 `json:"max_insert_serq"`

	// Shards the key of this map is the host name of the owner of the
	// shard, and the value the shard
	Shards map[string]*Shard

	md *Model

	adquiredInGroup bool

	table *dynamodb.Table
}

type ModelInt interface {
	GetAllGroups() map[string]map[string]*GroupInfo
	GetGroupById(userId, secret, groupId string) (gr *GroupInfo, err error)
	AddGroup(userId, secret, groupId string, numShards int, maxElements, maxReqSec, maxInsertReqSec uint64, maxScore uint8) (gr *GroupInfo, err error)
	ReleaseAllAdquiredShards()
	GetTotalNumberOfShards() (tot int)
}

type Model struct {
	ModelInt

	// groups by user ID and Group ID
	groups    map[string]map[string]*GroupInfo
	table     *dynamodb.Table
	tableName string
	mutex     sync.Mutex
	conn      *dynamodb.Server
}

func GetModel(prefix, awsRegion string) (md *Model) {
	if awsAuth, err := aws.EnvAuth(); err == nil {
		md = &Model{
			tableName: fmt.Sprintf("%s_%s", prefix, cTable),
			groups:    make(map[string]map[string]*GroupInfo),
			conn: &dynamodb.Server{
				Auth:   awsAuth,
				Region: aws.Regions[awsRegion],
			},
		}
		md.initTable()

		md.updateInfo()
		go func() {
			for {
				md.updateInfo()

				time.Sleep(time.Second * cUpdatePeriod)
			}
		}()
	} else {
		log.Error("Problem trying to connect with DynamoDB, Error:", err)
		return
	}

	return
}

func (md *Model) GetTotalNumberOfShards() (tot int) {
	md.mutex.Lock()
	for _, groups := range md.groups {
		for _, group := range groups {
			tot += group.NumShards
		}
	}
	md.mutex.Unlock()

	return
}

func (md *Model) GetAllGroups() map[string]map[string]*GroupInfo {
	return md.groups
}

func (md *Model) GetGroupById(userId, secret, groupId string) (gr *GroupInfo, err error) {
	if userGroups, ugOk := md.groups[userId]; ugOk {
		if group, grOk := userGroups[groupId]; grOk {
			if group.Secret == secret {
				return group, nil
			} else {
				return nil, CErrAuth
			}
		} else {
			return nil, CErrGroupNotFound
		}
	}

	return nil, CErrGroupUserNotFound
}

func (md *Model) AddGroup(userId, secret, groupId string, numShards int, maxElements, maxReqSec, maxInsertReqSec uint64, maxScore uint8) (gr *GroupInfo, err error) {
	if userGroups, ugOk := md.groups[userId]; ugOk {
		if group, grOk := userGroups[groupId]; grOk {
			return group, CErrGroupInUse
		}
	}

	gr = &GroupInfo{
		UserId:  userId,
		Secret:  secret,
		GroupId: groupId,

		NumShards: numShards,
		MaxScore:  maxScore,

		MaxElements:     maxElements,
		MaxReqSec:       maxReqSec,
		MaxInsertReqSec: maxInsertReqSec,

		Shards: make(map[string]*Shard),

		md: md,

		table: md.table,
	}

	log.Debug("Shards Max Shards:", numShards, gr.NumShards)
	if userGroups, ugOk := md.groups[userId]; ugOk {
		userGroups[groupId] = gr
	} else {
		md.groups[userId] = map[string]*GroupInfo{
			gr.GroupId: gr,
		}
	}

	return gr, gr.persist()
}

func (gr *GroupInfo) AcquireShard() (adquired bool, err error) {
	gr.consistentUpdate()

	if len(gr.Shards) >= gr.NumShards {
		log.Debug("Max number of shards allowed, can't adquire more")
		return false, CErrMaxShardsByGroup
	}

	if _, in := gr.Shards[instances.GetHostName()]; in {
		log.Debug("This instance owns a shard on this group:", gr.GroupId)
		return false, CSharPrevOwnedGroup
	}

	gr.Shards[instances.GetHostName()] = &Shard{
		Addr:   instances.GetHostName(),
		LastTs: uint32(time.Now().Unix()),

		ReqHour: []uint64{},
		ReqMin:  []uint64{},
		ReqSec:  0,
	}

	// Adquire
	gr.persist()
	gr.consistentUpdate()

	_, in := gr.Shards[instances.GetHostName()]
	gr.adquiredInGroup = true

	go gr.keepAliveOwnedShard(instances.GetHostName())

	return in, nil
}

func (gr *GroupInfo) keepAliveOwnedShard(hostName string) {
	for gr.adquiredInGroup {
		log.Debug("Updating shard TTL for group:", gr.GroupId, instances.GetHostName())
		gr.Shards[hostName].LastTs = uint32(time.Now().Unix())
		gr.persist()
		time.Sleep(time.Second * cUpdateShardPeriod)
	}
}

func (gr *GroupInfo) ReleaseAdquiredShard() {
	gr.adquiredInGroup = false
	gr.md.mutex.Lock()
	delete(gr.Shards, instances.GetHostName())
	gr.md.mutex.Unlock()
	gr.persist()
}

func (md *Model) ReleaseAllAdquiredShards() {
	for _, groupsByUser := range md.groups {
		for _, group := range groupsByUser {
			if _, ok := group.Shards[instances.GetHostName()]; ok {
				group.adquiredInGroup = false
				md.mutex.Lock()
				delete(group.Shards, instances.GetHostName())
				md.mutex.Unlock()
				group.persist()
			}
		}
	}
}

func (gr *GroupInfo) getPrimKey() string {
	return gr.UserId + ":" + gr.GroupId
}

func (gr *GroupInfo) consistentUpdate() (success bool) {
	attKey := &dynamodb.Key{
		HashKey:  gr.getPrimKey(),
		RangeKey: "",
	}
	if data, err := gr.table.GetItemConsistent(attKey, true); err == nil {
		gr.md.mutex.Lock()
		if err := json.Unmarshal([]byte(data["info"].Value), &gr); err != nil {
			log.Error("Problem trying to update the group information for group with ID:", gr.getPrimKey(), "Error:", err)
			gr.md.mutex.Unlock()
			return false
		}
		gr.md.mutex.Unlock()
	} else {
		log.Error("Problem trying to update the group information for group with ID:", gr.getPrimKey(), "Error:", err)
		return false
	}

	return true
}

func (gr *GroupInfo) persist() (err error) {
	if grJson, err := json.Marshal(gr); err == nil {
		log.Debug("Persist:", gr, string(grJson))
		attribs := []dynamodb.Attribute{
			*dynamodb.NewStringAttribute(cPrimKey, gr.getPrimKey()),
			*dynamodb.NewStringAttribute("info", string(grJson)),
		}

		if _, err := gr.table.PutItem(gr.getPrimKey(), cPrimKey, attribs); err != nil {
			log.Error("The group information for the group:", gr.getPrimKey(), " can't be persisted on Dynamo DB, Error:", err)
			return err
		}
		log.Debug("Group persisted:", gr.getPrimKey())
	} else {
		log.Error("The group info can't be converted to JSON, Erro:", err)

		return err
	}

	return
}

func (md *Model) updateInfo() {
	log.Debug("Updating grups info")
	updatedInfo := make(map[string]map[string]bool)
	if rows, err := md.table.Scan(nil); err == nil {
		md.mutex.Lock()
		for _, row := range rows {
			groupInfo := new(GroupInfo)
			if err := json.Unmarshal([]byte(row["info"].Value), &groupInfo); err != nil {
				log.Error("The returned data from Dynamo DB for the shards info can't be unmarshalled, Error:", err)
				continue
			}
			groupInfo.table = md.table
			if _, ok := md.groups[groupInfo.UserId]; ok {
				if _, issetGroup := md.groups[groupInfo.UserId][groupInfo.GroupId]; !issetGroup {
					md.groups[groupInfo.UserId][groupInfo.GroupId] = groupInfo
				}
			} else {
				md.groups[groupInfo.UserId] = map[string]*GroupInfo{
					groupInfo.GroupId: groupInfo,
				}
			}

			if _, ok := updatedInfo[groupInfo.UserId]; ok {
				updatedInfo[groupInfo.UserId][groupInfo.GroupId] = true
			} else {
				updatedInfo[groupInfo.UserId] = map[string]bool{
					groupInfo.GroupId: true,
				}
			}
		}

		// Remove the deprecated configuration
		for userId, userGroups := range md.groups {
			if _, issetUser := updatedInfo[userId]; issetUser {
				for groupId, _ := range userGroups {
					if _, issetGroup := updatedInfo[userId][groupId]; !issetGroup {
						log.Debug("Group:", groupId, "removed on user:", userId)
						delete(md.groups[userId], groupId)
					}
				}
			} else {
				log.Debug("User removed with ID:", userId)
				delete(md.groups, userId)
			}
		}
		md.mutex.Unlock()
		md.checkAndReleaseDeadShards()
	} else {
		log.Error("Problem trying to get the list of shards from Dynamo DB, Error:", err)
	}
}

func (md *Model) checkAndReleaseDeadShards() {
	for _, groups := range md.groups {
		for _, group := range groups {
			for hostname, shardInfo := range group.Shards {
				if shardInfo.LastTs+cShardTTL < uint32(time.Now().Unix()) {
					md.mutex.Lock()
					delete(group.Shards, hostname)
					group.persist()
					md.mutex.Unlock()
				}
			}
		}
	}
}

func (md *Model) initTable() {
	md.mutex.Lock()
	defer md.mutex.Unlock()

	pKey := dynamodb.PrimaryKey{dynamodb.NewStringAttribute(cPrimKey, ""), nil}
	md.table = md.conn.NewTable(md.tableName, pKey)
	log.Debug("Table:", md.table, md.tableName, cPrimKey)

	res, err := md.table.DescribeTable()
	if err != nil {
		log.Info("Creating a new table on DynamoDB:", md.tableName)
		td := dynamodb.TableDescriptionT{
			TableName: md.tableName,
			AttributeDefinitions: []dynamodb.AttributeDefinitionT{
				dynamodb.AttributeDefinitionT{cPrimKey, "S"},
			},
			KeySchema: []dynamodb.KeySchemaT{
				dynamodb.KeySchemaT{cPrimKey, "HASH"},
			},
			ProvisionedThroughput: dynamodb.ProvisionedThroughputT{
				ReadCapacityUnits:  cDefaultWRCapacity,
				WriteCapacityUnits: cDefaultWRCapacity,
			},
		}

		if _, err := md.conn.CreateTable(td); err != nil {
			log.Error("Error trying to create a table on Dynamo DB, table:", md.tableName, "Error:", err)
		}
		if res, err = md.table.DescribeTable(); err != nil {
			log.Error("Error trying to describe a table on Dynamo DB, table:", md.tableName, "Error:", err)
		}
	}

	for "ACTIVE" != res.TableStatus {
		if res, err = md.table.DescribeTable(); err != nil {
			log.Error("Can't describe Dynamo DB instances table, Error:", err)
		}
		log.Debug("Waiting for active table, current status:", res.TableStatus)
		time.Sleep(time.Second)
	}
}

func (md *Model) delTable() {
	if tableDesc, err := md.conn.DescribeTable(md.tableName); err == nil {
		if _, err = md.conn.DeleteTable(*tableDesc); err != nil {
			log.Error("Can't remove Dynamo table:", md.tableName, "Error:", err)
		}
	} else {
		log.Error("Can't remove Dynamo table:", md.tableName, "Error:", err)
	}
}

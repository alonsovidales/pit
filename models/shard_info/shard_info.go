package shardinfo

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/alonsovidales/pit/log"
	//"github.com/alonsovidales/pit/models/instances"
	"github.com/goamz/goamz/aws"
	"github.com/goamz/goamz/dynamodb"
	"sync"
	"time"
)

const (
	cGroupsTable             = "rec_groups"
	cGroupsPrimKey           = "groupId"
	cGroupsDefaultWRCapacity = 5

	cShardsTable             = "rec_shards"
	cShardsPrimKey           = "shardId"
	cShardsDefaultWRCapacity = 10

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
	GroupID   string `json:"group_id"`
	ShardID   int `json:"shard_id"`
	LastTs uint32 `json:"last_ts"`

	ReqHour []uint64 `json:"reqs_hour"`
	ReqMin  []uint64 `json:"reqs_min"`
	ReqSec  uint64   `json:"reqs_sec"`

	md *Model
}

type GroupInfo struct {
	GroupInfoInt `json:"-"`

	UserID  string `json:"user_id"`
	Secret  string `json:"secret"`
	GroupID string `json:"group_id"`

	// MaxScore The max score for the group elements
	MaxScore uint8 `json:"max_score"`
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
	Shards map[string]*Shard `json:"-"`
	ShardsByAddr map[string]*Shard `json:"-"`

	md *Model

	adquiredInGroup bool
}

type ModelInt interface {
	GetAllGroups() map[string]map[string]*GroupInfo
	GetGroupByUserKeyId(userId, secret, groupId string) (gr *GroupInfo, err error)
	AddGroup(userId, secret, groupId string, numShards int, maxElements, maxReqSec, maxInsertReqSec uint64, maxScore uint8) (gr *GroupInfo, err error)
	ReleaseAllAdquiredShards()
	GetTotalNumberOfShards() (tot int)
	RemoveGroup(groupId string) (err error)
}

type Model struct {
	ModelInt

	// groups by user ID and Group ID
	groups    map[string]map[string]*GroupInfo

	groupsTable     *dynamodb.Table
	shardsTable     *dynamodb.Table
	groupsTableName string
	shardsTableName string
	groupsMutex     sync.Mutex
	shardsMutex     sync.Mutex
	conn      *dynamodb.Server
}

func GetModel(prefix, awsRegion string) (md *Model) {
	if awsAuth, err := aws.EnvAuth(); err == nil {
		md = &Model{
			groupsTableName: fmt.Sprintf("%s_%s", prefix, cGroupsTable),
			shardsTableName: fmt.Sprintf("%s_%s", prefix, cShardsTable),
			groups:    make(map[string]map[string]*GroupInfo),
			conn: &dynamodb.Server{
				Auth:   awsAuth,
				Region: aws.Regions[awsRegion],
			},
		}
		md.groupsTable = md.getTable(md.groupsTableName, cGroupsPrimKey, cGroupsDefaultWRCapacity, md.groupsMutex)
		md.shardsTable = md.getTable(md.shardsTableName, cShardsPrimKey, cShardsDefaultWRCapacity, md.shardsMutex)

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

func (md *Model) AddGroup(userId, secret, groupId string, numShards int, maxElements, maxReqSec, maxInsertReqSec uint64, maxScore uint8) (gr *GroupInfo, err error) {
	if userGroups, ugOk := md.groups[userId]; ugOk {
		if group, grOk := userGroups[groupId]; grOk {
			return group, CErrGroupInUse
		}
	}

	gr = &GroupInfo{
		UserID:  userId,
		Secret:  secret,
		GroupID: groupId,

		NumShards: numShards,
		MaxScore:  maxScore,

		MaxElements:     maxElements,
		MaxReqSec:       maxReqSec,
		MaxInsertReqSec: maxInsertReqSec,

		Shards: make(map[string]*Shard),

		md: md,
	}

	log.Debug("Shards Max Shards:", numShards, gr.NumShards)
	if userGroups, ugOk := md.groups[userId]; ugOk {
		userGroups[groupId] = gr
	} else {
		md.groups[userId] = map[string]*GroupInfo{
			gr.GroupID: gr,
		}
	}

	for i := 0; i < numShards; i++ {
		gr.addShard(i)
	}

	return gr, gr.persist()
}

func (md *Model) updateInfo() {
	log.Debug("Updating grups info...")
	// This structure will be used in order to determine what groups are
	// still in use and what not
	updatedInfo := make(map[string]map[string]bool)
	if groupsRows, err := md.groupsTable.Scan(nil); err == nil {
		if shardsRows, err := md.shardsTable.Scan(nil); err == nil {
			md.shardsMutex.Lock()

			shardInfoByGroup := make(map[string]map[string]*Shard)
			for _, shardInfoRow := range shardsRows {
				shardInfo := new(Shard)
				if err := json.Unmarshal([]byte(shardInfoRow["info"].Value), &shardInfo); err != nil {
					log.Error("The returned data from Dynamo DB for the shards info can't be unmarshalled, Error:", err)
					continue
				}
				shardIDStr := fmt.Sprintf("%d", shardInfo.ShardID)
				if _, ok := shardInfoByGroup[shardInfo.GroupID]; ok {
					shardInfoByGroup[shardInfo.GroupID][shardIDStr] = shardInfo
				} else {
					shardInfoByGroup[shardInfo.GroupID] = map[string]*Shard{
						shardIDStr: shardInfo,
					}
				}
			}

			md.groupsMutex.Lock()

			md.groups = make(map[string]map[string]*GroupInfo)
			for _, groupInfoRow := range groupsRows {
				groupInfo := new(GroupInfo)
				if err := json.Unmarshal([]byte(groupInfoRow["info"].Value), &groupInfo); err != nil {
					log.Error("The returned data from Dynamo DB for the shards info can't be unmarshalled, Error:", err)
					continue
				}

				groupInfo.md = md
				groupInfo.Shards = shardInfoByGroup[groupInfo.GroupID]
				groupInfo.ShardsByAddr = make(map[string]*Shard)
				for _, shard := range groupInfo.Shards {
					if shard.Addr != "" {
						groupInfo.ShardsByAddr[shard.Addr] = shard
					}
				}

				if _, ok := md.groups[groupInfo.UserID]; ok {
					md.groups[groupInfo.UserID][groupInfo.GroupID] = groupInfo
				} else {
					md.groups[groupInfo.UserID] = map[string]*GroupInfo{
						groupInfo.GroupID: groupInfo,
					}
				}

				if _, ok := updatedInfo[groupInfo.UserID]; ok {
					updatedInfo[groupInfo.UserID][groupInfo.GroupID] = true
				} else {
					updatedInfo[groupInfo.UserID] = map[string]bool{
						groupInfo.GroupID: true,
					}
				}
			}

			md.groupsMutex.Unlock()
			md.shardsMutex.Unlock()

			log.Debug("UPDATE Result:", md.groups["userId"]["groupId"])
		} else {
			log.Error("Problem trying to get the list of shards from Dynamo DB, Error:", err)
		}
	} else {
		log.Error("Problem trying to get the list of groups from Dynamo DB, Error:", err)
	}
}

func (gr *GroupInfo) addShard(shardId int) (shard *Shard, err error) {
	shard = &Shard {
		GroupID: gr.GroupID,
		ShardID: shardId,
		md: gr.md,
	}

	shard.persist()

	return
}

func (sh *Shard) persist() (err error) {
	// Persis the shard row
	if shJson, err := json.Marshal(sh); err == nil {
		log.Debug("Persisting shard:", sh, string(shJson))
		keyStr := fmt.Sprintf("%d", sh.ShardID)
		attribs := []dynamodb.Attribute{
			*dynamodb.NewStringAttribute(cShardsPrimKey, keyStr),
			*dynamodb.NewStringAttribute("info", string(shJson)),
		}

		if _, err := sh.md.shardsTable.PutItem(keyStr, cShardsPrimKey, attribs); err != nil {
			log.Error("The shard information for the shard of the group:", sh.GroupID, "And Shard ID:", sh.ShardID, " can't be persisted on Dynamo DB, Error:", err)
			return err
		}
		log.Debug("Shard persisted:", sh.ShardID)
	} else {
		log.Error("The shard info can't be converted to JSON, Erro:", err)

		return err
	}

	return
}

func (gr *GroupInfo) persist() (err error) {
	// Persis the groups row
	if grJson, err := json.Marshal(gr); err == nil {
		log.Debug("Persisting group:", gr, string(grJson))
		attribs := []dynamodb.Attribute{
			*dynamodb.NewStringAttribute(cGroupsPrimKey, gr.GroupID),
			*dynamodb.NewStringAttribute("info", string(grJson)),
		}

		if _, err := gr.md.groupsTable.PutItem(gr.GroupID, cGroupsPrimKey, attribs); err != nil {
			log.Error("The group information for the group:", gr.GroupID, " can't be persisted on Dynamo DB, Error:", err)
			return err
		}
		log.Debug("Group persisted:", gr.GroupID)
	} else {
		log.Error("The group info can't be converted to JSON, Erro:", err)

		return err
	}

	return
}

func (md *Model) getTable(tName, tPrimKey string, rwCapacity int64, mutex sync.Mutex) (table *dynamodb.Table) {
	mutex.Lock()
	defer mutex.Unlock()

	pKey := dynamodb.PrimaryKey{dynamodb.NewStringAttribute(tPrimKey, ""), nil}
	table = md.conn.NewTable(tName, pKey)

	res, err := table.DescribeTable()
	if err != nil {
		log.Info("Creating a new table on DynamoDB:", tName)
		td := dynamodb.TableDescriptionT{
			TableName: tName,
			AttributeDefinitions: []dynamodb.AttributeDefinitionT{
				dynamodb.AttributeDefinitionT{tPrimKey, "S"},
			},
			KeySchema: []dynamodb.KeySchemaT{
				dynamodb.KeySchemaT{tPrimKey, "HASH"},
			},
			ProvisionedThroughput: dynamodb.ProvisionedThroughputT{
				ReadCapacityUnits:  rwCapacity,
				WriteCapacityUnits: rwCapacity,
			},
		}

		if _, err := md.conn.CreateTable(td); err != nil {
			log.Error("Error trying to create a table on Dynamo DB, table:", tName, "Error:", err)
		}
		if res, err = table.DescribeTable(); err != nil {
			log.Error("Error trying to describe a table on Dynamo DB, table:", tName, "Error:", err)
		}
	}

	for "ACTIVE" != res.TableStatus {
		if res, err = table.DescribeTable(); err != nil {
			log.Error("Can't describe Dynamo DB instances table, Error:", err)
		}
		log.Debug("Waiting for active table, current status:", res.TableStatus)
		time.Sleep(time.Second)
	}

	return
}

func (md *Model) delTables() {
	if tableDesc, err := md.conn.DescribeTable(md.shardsTableName); err == nil {
		if _, err = md.conn.DeleteTable(*tableDesc); err != nil {
			log.Error("Can't remove Dynamo table:", md.shardsTableName, "Error:", err)
		}
	} else {
		log.Error("Can't remove Dynamo table:", md.shardsTableName, "Error:", err)
	}
	if tableDesc, err := md.conn.DescribeTable(md.groupsTableName); err == nil {
		if _, err = md.conn.DeleteTable(*tableDesc); err != nil {
			log.Error("Can't remove Dynamo table:", md.groupsTableName, "Error:", err)
		}
	} else {
		log.Error("Can't remove Dynamo table:", md.groupsTableName, "Error:", err)
	}
}

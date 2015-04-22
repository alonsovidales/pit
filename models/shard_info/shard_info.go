package shardinfo

import (
	"encoding/json"
	"errors"
	"fmt"
	"github.com/alonsovidales/pit/log"
	"github.com/alonsovidales/pit/models/instances"
	"github.com/alonsovidales/pit/recommender"
	"github.com/goamz/goamz/aws"
	"github.com/goamz/goamz/dynamodb"
	"github.com/nu7hatch/gouuid"
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

	cUpdatePeriod      = 5
	cUpdateShardPeriod = 2
	cShardTTL          = 10
)

var ErrGroupUserNotFound = errors.New("User not found")
var ErrGroupInUse = errors.New("Group ID in use")
var ErrGroupNotFound = errors.New("Group not found")
var ErrAuth = errors.New("Authentication problem")
var ErrMaxShardsByGroup = errors.New("Max number of shards for this group reached")
var ErrSharPrevOwnedGroup = errors.New("This instance yet owns a shard on this group")

type GroupInfoInt interface {
	AcquireShard() (adquired bool, err error)
	ReleaseShard()
	IsThisInstanceOwner() bool
	RegenerateKey() (key string, err error)
	SetNumShards(numShards int) error
}

type Shard struct {
	Addr    string `json:"addr"`
	GroupID string `json:"group_id"`
	ShardID int    `json:"shard_id"`
	LastTs  int64  `json:"last_ts"`

	expire bool

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
	Shards       map[int]*Shard    `json:"-"`
	ShardsByAddr map[string]*Shard `json:"-"`

	md *Model
}

type ModelInt interface {
	GetAllGroups() map[string]map[string]*GroupInfo
	GetAllGroupsByUserID(uid string) map[string]*GroupInfo
	GetGroupByUserKeyID(userID, secret, groupID string) (gr *GroupInfo, err error)
	GetGroupByID(groupID string) (gr *GroupInfo)
	AddUpdateGroup(userID, groupID string, numShards int, maxElements, maxReqSec, maxInsertReqSec uint64, maxScore uint8) (gr *GroupInfo, key string, err error)
	ReleaseAllAcquiredShards()
	GetTotalNumberOfShards() (tot int)
	RemoveGroup(groupID string) (err error)
}

type Model struct {
	// groups by user ID and Group ID
	groups map[string]map[string]*GroupInfo

	groupsTable     *dynamodb.Table
	shardsTable     *dynamodb.Table
	groupsTableName string
	shardsTableName string
	adminEmail      string
	groupsMutex     sync.Mutex
	shardsMutex     sync.Mutex
	conn            *dynamodb.Server
}

func GetModel(prefix, awsRegion, adminEmail string) (md *Model) {
	if awsAuth, err := aws.EnvAuth(); err == nil {
		md = &Model{
			groupsTableName: fmt.Sprintf("%s_%s", prefix, cGroupsTable),
			shardsTableName: fmt.Sprintf("%s_%s", prefix, cShardsTable),
			groups:          make(map[string]map[string]*GroupInfo),
			adminEmail:      adminEmail,
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

func (md *Model) AddUpdateGroup(userID, groupID string, numShards int, maxElements, maxReqSec, maxInsertReqSec uint64, maxScore uint8) (gr *GroupInfo, key string, err error) {
	var grOk bool
	userGroups, ugOk := md.groups[userID]
	if gr, grOk = userGroups[groupID]; ugOk && grOk {
		gr.MaxScore = maxScore
		gr.MaxElements = maxElements
		gr.MaxReqSec = maxReqSec
		gr.MaxInsertReqSec = maxInsertReqSec

		if numShards > gr.NumShards {
			for i := gr.NumShards; i < numShards; i++ {
				gr.addShard(i)
			}
		}

		gr.NumShards = numShards
	} else {
		gr = &GroupInfo{
			UserID:  userID,
			GroupID: groupID,

			NumShards: numShards,
			MaxScore:  maxScore,

			MaxElements:     maxElements,
			MaxReqSec:       maxReqSec,
			MaxInsertReqSec: maxInsertReqSec,

			Shards: make(map[int]*Shard),

			md: md,
		}

		gr.RegenerateKey()

		if userGroups, ugOk := md.groups[userID]; ugOk {
			userGroups[groupID] = gr
		} else {
			md.groups[userID] = map[string]*GroupInfo{
				gr.GroupID: gr,
			}
		}

		for i := 0; i < numShards; i++ {
			gr.addShard(i)
		}
	}

	return gr, gr.Secret, gr.persist()
}

func (md *Model) GetGroupByID(groupID string) (gr *GroupInfo) {
	md.groupsMutex.Lock()
	defer md.groupsMutex.Unlock()

	for _, groups := range md.groups {
		for _, group := range groups {
			if group.GroupID == groupID {
				return group
			}
		}
	}

	return
}

func (md *Model) GetAllGroupsByUserID(uid string) map[string]*GroupInfo {
	if uid == md.adminEmail {
		result := make(map[string]*GroupInfo)
		for _, userGroups := range md.groups {
			for k, v := range userGroups {
				result[k] = v
			}
		}

		return result
	}

	return md.groups[uid]
}

func (md *Model) GetAllGroups() map[string]map[string]*GroupInfo {
	return md.groups
}

func (md *Model) GetGroupByUserKeyID(userID, secret, groupID string) (gr *GroupInfo, err error) {
	md.groupsMutex.Lock()
	defer md.groupsMutex.Unlock()

	if userID == md.adminEmail {
		for _, groups := range md.groups {
			for gID, group := range groups {
				if gID == groupID && secret == group.Secret {
					return group, nil
				}
			}
		}
	}

	if userGroups, ugOk := md.groups[userID]; ugOk {
		if group, grOk := userGroups[groupID]; grOk {
			if group.Secret == secret {
				log.Debug("Group found:", group)
				return group, nil
			}
			return nil, ErrAuth
		}
		return nil, ErrGroupNotFound
	}

	return nil, ErrGroupUserNotFound
}

func (gr *GroupInfo) RemoveAllContent(rec *recommender.Recommender) bool {
	prevShards := gr.NumShards
	if rec.DestroyS3Backup() {
		// Force the release of all the shards on this group to take them again
		// after the S3 backup is removed
		gr.NumShards = 0
		gr.persist()
		time.Sleep(10 * time.Second)
		gr.NumShards = prevShards
		gr.persist()

		return true
	}

	return false
}

func (gr *GroupInfo) SetNumShards(numShards int) error {
	if numShards > gr.NumShards {
		for i := gr.NumShards; i < numShards; i++ {
			gr.addShard(i)
		}
	}

	gr.NumShards = numShards

	return gr.persist()
}

func (gr *GroupInfo) RegenerateKey() (key string, err error) {
	secret, _ := uuid.NewV4()
	gr.Secret = secret.String()

	return gr.Secret, gr.persist()
}

func (gr *GroupInfo) IsThisInstanceOwner() bool {
	_, is := gr.ShardsByAddr[instances.GetHostName()]

	return is
}

func (gr *GroupInfo) AcquireShard() (adquired bool, err error) {
	if len(gr.ShardsByAddr) == len(gr.Shards) {
		log.Debug("Max number of shards allowed, can't adquire more")
		return false, ErrMaxShardsByGroup
	}

	if _, in := gr.ShardsByAddr[instances.GetHostName()]; in {
		log.Debug("This instance owns a shard on this group:", gr.GroupID)
		return false, ErrSharPrevOwnedGroup
	}

	// Get a free shard
	var shard *Shard
	found := false
	for _, shard = range gr.Shards {
		// Double check in order to be as sure as possible that this
		// shard is still available
		if shard.Addr == "" || shard.LastTs+cShardTTL < time.Now().Unix() {
			shard.consistentUpdate()
			if shard.Addr == "" || shard.LastTs+cShardTTL < time.Now().Unix() {
				found = true
				break
			}
		}
	}
	if !found {
		return false, errors.New("Consistency error trying to adquire")
	}

	shard.Addr = instances.GetHostName()
	shard.LastTs = time.Now().Unix()

	// Adquire
	shard.persist()
	// Wait just by caution in order to avoid race conditions between
	// instances on the revious call
	time.Sleep(time.Millisecond * 200)
	shard.consistentUpdate()

	if shard.Addr == instances.GetHostName() {
		go gr.md.keepAliveOwnedShard(gr.GroupID, instances.GetHostName())
		gr.ShardsByAddr[instances.GetHostName()] = shard

		return true, nil
	}

	return false, errors.New(fmt.Sprint("Race condition trying to adquire the shard:", gr.GroupID, ":", shard.ShardID))
}

func (md *Model) keepAliveOwnedShard(groupID string, hostName string) {
	for {
		gr := md.GetGroupByID(groupID)
		if shard, ok := gr.ShardsByAddr[hostName]; ok {
			shard.LastTs = time.Now().Unix()
			shard.persist()
			time.Sleep(time.Second * cUpdateShardPeriod)
		} else {
			return
		}
	}
}

func (md *Model) updateInfo() {
	// This structure will be used in order to determine what groups are
	// still in use and what not
	updatedInfo := make(map[string]map[string]bool)
	if groupsRows, err := md.groupsTable.Scan(nil); err == nil {
		if shardsRows, err := md.shardsTable.Scan(nil); err == nil {
			md.shardsMutex.Lock()

			shardInfoByGroup := make(map[string]map[int]*Shard)
			for _, shardInfoRow := range shardsRows {
				shardInfo := new(Shard)
				if err := json.Unmarshal([]byte(shardInfoRow["info"].Value), &shardInfo); err != nil {
					log.Error("The returned data from Dynamo DB for the shards info can't be unmarshalled, Error:", err)
					continue
				}
				shardInfo.md = md
				if _, ok := shardInfoByGroup[shardInfo.GroupID]; ok {
					shardInfoByGroup[shardInfo.GroupID][shardInfo.ShardID] = shardInfo
				} else {
					shardInfoByGroup[shardInfo.GroupID] = map[int]*Shard{
						shardInfo.ShardID: shardInfo,
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

				groupInfo.Shards = make(map[int]*Shard)
				for k, v := range shardInfoByGroup[groupInfo.GroupID] {
					if k < groupInfo.NumShards {
						groupInfo.Shards[k] = v
					}
				}

				groupInfo.ShardsByAddr = make(map[string]*Shard)
				for _, shard := range groupInfo.Shards {
					if shard.Addr != "" && shard.LastTs+cShardTTL >= time.Now().Unix() && shard.ShardID < groupInfo.NumShards {
						groupInfo.ShardsByAddr[shard.Addr] = shard
					} else {
						// This shard ownership has expired, release it
						shard.Addr = ""
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
		} else {
			log.Error("Problem trying to get the list of shards from Dynamo DB, Error:", err)
		}
	} else {
		log.Error("Problem trying to get the list of groups from Dynamo DB, Error:", err)
	}
}

func (gr *GroupInfo) addShard(shardID int) (shard *Shard, err error) {
	shard = &Shard{
		GroupID: gr.GroupID,
		ShardID: shardID,
		md:      gr.md,
		expire:  false,
	}

	shard.persist()
	gr.Shards[shardID] = shard

	return
}

func (gr *GroupInfo) delShard(shardID int) {
	shard := &Shard{
		GroupID: gr.GroupID,
		ShardID: shardID,
		md:      gr.md,
		expire:  true,
	}

	shard.persist()
	delete(gr.Shards, shardID)
}

func (sh *Shard) getDynamoDbKey() string {
	return fmt.Sprintf("%s:%d", sh.GroupID, sh.ShardID)
}

func (sh *Shard) consistentUpdate() (success bool) {
	attKey := &dynamodb.Key{
		HashKey:  sh.getDynamoDbKey(),
		RangeKey: "",
	}
	if data, err := sh.md.shardsTable.GetItemConsistent(attKey, true); err == nil {
		log.Debug("Consistent update shard:", sh.GroupID, sh.ShardID)
		sh.md.shardsMutex.Lock()
		defer sh.md.shardsMutex.Unlock()
		if err := json.Unmarshal([]byte(data["info"].Value), &sh); err != nil {
			log.Error("Problem trying to update the shard information for shard in group ID:", sh.GroupID, "and shard ID:", sh.ShardID, "Error:", err)
			return false
		}
	} else {
		log.Error("Problem trying to update the shard information for shard in group ID:", sh.GroupID, "and shard ID:", sh.ShardID, "Error:", err)
		return false
	}

	return true
}

func (sh *Shard) persist() (err error) {
	// Persis the shard row
	if shJSON, err := json.Marshal(sh); err == nil {
		keyStr := sh.getDynamoDbKey()
		attribs := []dynamodb.Attribute{
			*dynamodb.NewStringAttribute(cShardsPrimKey, keyStr),
			*dynamodb.NewStringAttribute("info", string(shJSON)),
		}

		if sh.expire {
			attribs = append(attribs, *dynamodb.NewStringAttribute("expire", "1"))
		}

		if _, err := sh.md.shardsTable.PutItem(keyStr, cShardsPrimKey, attribs); err != nil {
			log.Error("The shard information for the shard of the group:", sh.GroupID, "And Shard ID:", sh.ShardID, " can't be persisted on Dynamo DB, Error:", err)
			return err
		}
	} else {
		log.Error("The shard info can't be converted to JSON, Erro:", err)

		return err
	}

	return
}

func (md *Model) GetTotalNumberOfShards() (tot int) {
	md.groupsMutex.Lock()
	for _, groups := range md.groups {
		for _, group := range groups {
			tot += group.NumShards
		}
	}
	md.groupsMutex.Unlock()

	return
}

func (gr *GroupInfo) persist() (err error) {
	// Persis the groups row
	if grJSON, err := json.Marshal(gr); err == nil {
		log.Debug("Persisting group:", gr, string(grJSON))
		attribs := []dynamodb.Attribute{
			*dynamodb.NewStringAttribute(cGroupsPrimKey, gr.GroupID),
			*dynamodb.NewStringAttribute("info", string(grJSON)),
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

func (md *Model) RemoveGroup(groupID string) (err error) {
	attKey := &dynamodb.Key{
		HashKey:  groupID,
		RangeKey: "",
	}

	_, err = md.groupsTable.DeleteItem(attKey)

	return
}

func (md *Model) ReleaseAllAcquiredShards() {
	md.groupsMutex.Lock()
	defer md.groupsMutex.Unlock()

	for _, groups := range md.groups {
		for _, group := range groups {
			if shard, ok := group.ShardsByAddr[instances.GetHostName()]; ok {
				shard.Addr = ""
				shard.persist()
			}

			delete(group.ShardsByAddr, instances.GetHostName())
			group.persist()
		}
	}
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

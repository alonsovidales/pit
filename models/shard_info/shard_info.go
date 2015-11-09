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

// ErrGroupUserNotFound User not found on the system
var ErrGroupUserNotFound = errors.New("User not found")

// ErrGroupInUse The specified group ID is already in use
var ErrGroupInUse = errors.New("Group ID in use")

// ErrGroupNotFound The group wwas not found on the system
var ErrGroupNotFound = errors.New("Group not found")

// ErrAuth Problem trying to authenticate the user
var ErrAuth = errors.New("Authentication problem")

// ErrMaxShardsByGroup The max number of shards had been reached for this group
// and can't be adquired anymore shards
var ErrMaxShardsByGroup = errors.New("Max number of shards for this group reached")

// ErrSharPrevOwnedGroup The current local machine already have adquired an
// instance on this group
var ErrSharPrevOwnedGroup = errors.New("This instance already owns a shard on this group")

// GroupInfoInt Interface that provides access to management of a group
type GroupInfoInt interface {
	AcquireShard() (adquired bool, err error)
	GetUserID()
	ReleaseShard()
	IsThisInstanceOwner() bool
	RegenerateKey() (key string, err error)
	SetNumShards(numShards int) error
}

// Shard Defines the shard information that is persisted on the DB
type Shard struct {
	// Addr Current address of the local host
	Addr string `json:"addr"`
	// GroupID ID of the group
	GroupID string `json:"group_id"`
	// ShardID ID of the shard
	ShardID int `json:"shard_id"`
	// LastTs Last time stamp when the shard information was updated
	LastTs int64 `json:"last_ts"`

	expire bool
	md     *Model
}

// GroupInfo Defined the information to be stored on this group
type GroupInfo struct {
	// UserID Id of the user
	UserID string `json:"user_id"`
	// Secret to be used to access to this group
	Secret string `json:"secret"`
	// GroupID OD of the group
	GroupID string `json:"group_id"`

	// Type group type, will be used for billing proposals
	Type string `json:"type"`
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
	Shards map[int]*Shard `json:"-"`
	// ShardsByAddr List od shards bu host
	ShardsByAddr map[string]*Shard `json:"-"`

	md *Model
}

// ModelInt manages the access to the stored information of a shard
type ModelInt interface {
	// GetAllGroups Returns all the groups of shards by user
	GetAllGroups() map[string]map[string]*GroupInfo
	// GetAllGroupsByUserID Returns all the groups of shards for a single user
	GetAllGroupsByUserID(uid string) map[string]*GroupInfo
	// GetGroupByUserKeyID Returns a group by a specified UserID checking
	// out if the key is correct
	GetGroupByUserKeyID(userID, secret, groupID string) (gr *GroupInfo, err error)
	// GetGroupByID Returns a group by Group ID
	GetGroupByID(groupID string) (gr *GroupInfo)
	// AddUpdateGroup Creates a group based on the provided information, or
	// updated the information on an existing group
	AddUpdateGroup(grType, userID, groupID string, numShards int, maxElements, maxReqSec, maxInsertReqSec uint64, maxScore uint8) (gr *GroupInfo, key string, err error)
	// ReleaseAllAcquiredShards Releases all the adquired shards for this
	// instance and returns the number of released shards
	ReleaseAllAcquiredShards()
	// GetTotalNumberOfShards Returns the total number of shards in general
	GetTotalNumberOfShards() (tot int)
	// RemoveGroup Removes a group by ID and all the information of this group
	RemoveGroup(groupID string) (err error)
}

// Model Manages all the information relative to a shard
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

// GetModel Initializes a new model and launches a process that getting the
// information from the DB keeps updated all this in memory
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
		md.groupsTable = md.getTable(
			md.groupsTableName,
			cGroupsPrimKey,
			cGroupsDefaultWRCapacity,
			md.groupsMutex,
		)
		md.shardsTable = md.getTable(
			md.shardsTableName,
			cShardsPrimKey,
			cShardsDefaultWRCapacity,
			md.shardsMutex,
		)

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

// AddUpdateGroup Creates a group based on the provided information, or updated
// the information on an existing group
func (md *Model) AddUpdateGroup(grType, userID, groupID string, numShards int, maxElements, maxReqSec, maxInsertReqSec uint64, maxScore uint8) (gr *GroupInfo, key string, err error) {
	var grOk bool

	userGroups, ugOk := md.groups[userID]
	if gr, grOk = userGroups[groupID]; ugOk && grOk {
		gr.Type = grType
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

			Type:            grType,
			MaxElements:     maxElements,
			MaxReqSec:       maxReqSec,
			MaxInsertReqSec: maxInsertReqSec,

			Shards:       make(map[int]*Shard),
			ShardsByAddr: make(map[string]*Shard),

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

// GetGroupByID Returns a group by Group ID
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

// GetAllGroupsByUserID Returns all the groups of shards for a single user
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

// GetAllGroups Returns all the groups of shards by user
func (md *Model) GetAllGroups() map[string]map[string]*GroupInfo {
	return md.groups
}

// GetGroupByUserKeyID Returns a group by a specified UserID checking out if
// the key is correct
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

// GetUserID Returns the User ID owner of the group
func (gr *GroupInfo) GetUserID() string {
	return gr.UserID
}

// RemoveAllContent Removes all the content for a group, removing also all the
// persisted information
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

// SetNumShards Sets the number of shards to be used on a group
func (gr *GroupInfo) SetNumShards(numShards int) error {
	if numShards > gr.NumShards {
		for i := gr.NumShards; i < numShards; i++ {
			gr.addShard(i)
		}
	}

	gr.NumShards = numShards

	return gr.persist()
}

// RegenerateKey Regenerates a random key for a group
func (gr *GroupInfo) RegenerateKey() (key string, err error) {
	secret, _ := uuid.NewV4()
	gr.Secret = secret.String()

	return gr.Secret, gr.persist()
}

// IsThisInstanceOwner Returns is the current host owns an instance of this
// group
func (gr *GroupInfo) IsThisInstanceOwner() bool {
	_, is := gr.ShardsByAddr[instances.GetHostName()]

	return is
}

// AcquireShard Try to adquire a shard on the current group, this action is
// based on a competition strategy to adquire the shard in a persistance system
// with eventual consistency as DynamoDB
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

// keepAliveOwnedShard Updates the timestamp of an adquired shard in order to
// inform to the other hosts that the ownership is still valid
func (md *Model) keepAliveOwnedShard(groupID string, hostName string) {
	for {
		if gr := md.GetGroupByID(groupID); gr == nil {
			break
		} else {
			if shard, ok := gr.ShardsByAddr[hostName]; ok {
				shard.LastTs = time.Now().Unix()
				shard.persist()
				time.Sleep(time.Second * cUpdateShardPeriod)
			} else {
				break
			}
		}
	}
}

// updateInfo syncronize the information in memory with the information on the
// DB
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

// addShard Adds a new shard to the goup and persist the group status
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

// delShard Removes a shard of the persistance layer
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

// getDynamoDbKey Returns the string that will identify a shard on the DB
func (sh *Shard) getDynamoDbKey() string {
	return fmt.Sprintf("%s:%d", sh.GroupID, sh.ShardID)
}

// consistentUpdate Performs an update on an eventual consistent DB as DynamoDB
// making it consistent based on a wait and ask strategy
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

// persist Persists on the DB the information of the shard
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

// GetTotalNumberOfShards Returns the total number of shards in general
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

// persist Persists the information of the group on the DB
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

// RemoveGroup Removes a group by ID and all the information of this group
func (md *Model) RemoveGroup(groupID string) (err error) {
	attKey := &dynamodb.Key{
		HashKey:  groupID,
		RangeKey: "",
	}

	gr := md.GetGroupByID(groupID)
	for i := 0; i < gr.NumShards; i++ {
		shardAttKey := &dynamodb.Key{
			HashKey:  fmt.Sprintf("%s:%d", gr.GroupID, i),
			RangeKey: "",
		}
		md.shardsTable.DeleteItem(shardAttKey)
	}
	_, err = md.groupsTable.DeleteItem(attKey)

	return
}

// ReleaseAllAcquiredShards Releases all the adquired shards for this instance
// and returns the number of released shards
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

// getTable Returns a Dynamo table and in case of this table don't being
// defined, creates it
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

// delTables Removes all the tables, method used for testing proposals only
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

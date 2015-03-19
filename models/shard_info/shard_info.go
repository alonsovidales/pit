package shardinfo

import (
	"github.com/goamz/goamz/dynamodb"
	"github.com/goamz/goamz/aws"
	"github.com/alonsovidales/pit/log"
	"errors"
	"fmt"
	"encoding/json"
	"time"
	"sync"
)

const (
	cTable             = "rec_shards"
	cPrimKey           = "groupId"
	cDefaultWRCapacity = 5
	cTTL               = 5

	cMaxHoursToStore = 168 // A week
	cUpdatePeriod    = 5
	cShardTTL        = 5
)

var CErrGroupNotFound = errors.New("Group not found")
var CErrAuth          = errors.New("Authentication problem")

type GroupInfoInt interface {
	IncReqSec() (reqs uint64)
	GetID() (id string)
	GetStats() (reqSec uint64, reqMin []uint64, reqHour []uint64)
	GetAddrPort() (addr string, port int)
	IsMaster() bool
	GetLimits() (maxElements uint64, maxReqSec uint64, maxInsertReqSec uint64)
	GetNeighboursshards() (maxElements uint64, maxReqSec uint64, maxInsertReqSec uint64)
	AddShards(numOfShards int)
}

type shard struct {
	addr   string
	port   int
	lastTs uint32

	reqHour []uint64
	reqMin  []uint64
	reqSec  uint64
}

type GroupInfo struct {
	GroupInfoInt

	userId  string
	secret  string
	groupId string

	maxElements     uint64
	maxReqSec       uint64
	maxInsertReqSec uint64

	// Master-Master instances by shard
	shards [][2]*shard
}

type ModelInt interface {
	GetGroupById(userId, secret, groupId string) (gr *GroupInfo)
	AddGroup(userId, secret, groupId string, maxElements, maxReqSec, maxInsertReqSec uint64) (gr *GroupInfo)
}

type Model struct {
	ModelInt

	// groups by user ID and Group ID
	groups map[string]map[string]*GroupInfo
	table *dynamodb.Table
	tableName string
	mutex sync.Mutex
	conn *dynamodb.Server
}

func GetModel(prefix string, awsRegion string) (md *Model) {
	if awsAuth, err := aws.EnvAuth(); err == nil {
		md = &Model{
			tableName: fmt.Sprintf("%s_%s", prefix, cTable),
			groups: make(map[string]map[string]*GroupInfo),
			conn: &dynamodb.Server{
				Auth:   awsAuth,
				Region: aws.Regions[awsRegion],
			},
		}
		md.initTable()

		md.updateInfo()
		go func () {
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

func (md *Model) delTable() {
	if tableDesc, err := md.conn.DescribeTable(md.tableName); err == nil {
		if _, err = md.conn.DeleteTable(*tableDesc); err != nil {
			log.Error("Can't remove Dynamo table:", md.tableName, "Error:", err)
		}
	} else {
		log.Error("Can't remove Dynamo table:", md.tableName, "Error:", err)
	}
}

func (md *Model) updateInfo() {
	// TODO Dump local info
	var groups []*GroupInfo

	log.Debug("Updating grups")
	if rows, err := md.table.Scan(nil); err == nil {
		groups = make([]*GroupInfo, len(rows))
		for i, row := range rows {
			if err := json.Unmarshal([]byte(row["info"].Value), &groups[i]); err != nil {
				log.Error("The returned data from Dynamo DB for the shards info can't be unmarshalled, Error:", err)
			}
		}
	} else {
		log.Error("Problem trying to get the list of shards from Dynamo DB, Error:", err)
	}

	newGroups := make(map[string]map[string]*GroupInfo)
	for _, gr := range groups {
		if _, ok := md.groups[gr.userId]; ok {
			md.groups[gr.userId][gr.groupId] = gr
		} else {
			md.groups[gr.userId] = map[string]*GroupInfo {
				gr.groupId: gr,
			}
		}
	}

	md.mutex.Lock()
	md.groups = newGroups
	md.mutex.Unlock()
}

func (md *Model) initTable() {
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
	}

	for "ACTIVE" != res.TableStatus {
		if res, err = md.table.DescribeTable(); err != nil {
			log.Error("Can't describe Dynamo DB instances table, Error:", err)
		}
		log.Debug("Waiting for active table, current status:", res.TableStatus)
		time.Sleep(time.Second)
	}
}

package shardinfo

import (
	"github.com/goamz/goamz/dynamodb"
)

type GroupInfoInt interface {
	IncReqSec() (reqs uint64)
	GetID() (id string)
	GetStats() (reqSec uint64, reqMin []uint64, reqHour []uint64)
	GetAddrPort() (addr string, port int)
	IsMaster() bool
	GetLimits() (maxElements uint64, maxReqSec uint64, maxInsertReqSec uint64)
	GetNeighboursshards() (maxElements uint64, maxReqSec uint64, maxInsertReqSec uint64)
}

type Stats struct {
	reqHour []uint64
	reqMin  []uint64
	reqSec  uint64
}

type shard struct {
	addr   string
	port   int
	lastTs uint32
	stats  *Stats
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

const (
	cMaxHoursToStore = 168 // A week
	cUpdatePeriod    = 5
	cShardTTL        = 5

	CErrGroupNotFound = errors.New("Group not found")
	CErrAuth          = errors.New("Authentication problem")
)

var conn *dynamodb.Server
var shards = make(map[string]map[string]*GroupInfo)

func GetNewGroup(prefix string, awsRegion string) (si *GroupInfo) {
	si = &GroupInfo{}
}

func tryToAcquireShard() {
	// Suscribe instance on shard
	// Wait at least T*3
	// Check if we are the owners of this shard
	// In that case, return true
}

// Returns the list of shards for a user group
func GetShardsByUserGroup(userID, secret, groupId string) (group *GroupInfo, err error) {
	if shards, auth := shards[getKey(userID, secret)]; auth {
		if group, issetGroup := shards[groupId]; group {
			return group, nil
		} else {
			return nil, CErrGroupNotFound
		}
	}

	return nil, CErrAuth
}

// StartCollector Collects each cUpdatePeriod seconds the data from DynamoDB about the shards, and removes the expired
func StartCollector(awsRegion string) {
	region := aws.Regions[awsRegion]
	for {
		conn := getConn(region)

		// Update data from Dynamo
		// Check the TTLs for all the shards and remove the expired shards
		// Try to adquire as many shads as we can and where we are not part of the shards

		time.Sleep(time.Second * cUpdatePeriod)
	}
}

func getKey(userID, secret string) string {
	return userID + secret
}

func getConn() *dynamodb.Server {
	if conn == nil {
		awsAuth, err := aws.EnvAuth()
		if err != nil {
			logger.Fatal("can't do log-in in AWS:", err)
		}

		conn = dynamodb.New(awsAuth, region)
	}

	return conn
}

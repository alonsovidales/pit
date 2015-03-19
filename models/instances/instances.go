package instances

import (
	"fmt"
	"github.com/alonsovidales/pit/log"
	"github.com/goamz/goamz/aws"
	"github.com/goamz/goamz/dynamodb"
	"os"
	"strconv"
	"sync"
	"time"
)

const (
	cTable             = "instances"
	cPrimKey           = "hostName"
	cDefaultWRCapacity = 5
	cTTL               = 5
)

type InstancesModel struct {
	prefix         string
	table          *dynamodb.Table
	instancesAlive []string
	conn           *dynamodb.Server
	tableName      string
	mutex          sync.Mutex
}

func InitAndKeepAlive(prefix string, awsRegion string) (im *InstancesModel) {
	if awsAuth, err := aws.EnvAuth(); err == nil {
		im = &InstancesModel{
			prefix:    prefix,
			tableName: fmt.Sprintf("%s_%s", prefix, cTable),
			conn: &dynamodb.Server{
				Auth:   awsAuth,
				Region: aws.Regions[awsRegion],
			},
		}
		im.initTable()

		im.updateInstances()
		go func() {
			for {
				im.updateInstances()
				time.Sleep(time.Second)
			}
		}()
	} else {
		log.Error("Problem trying to connect with DynamoDB, Error:", err)
		return
	}

	return
}

func (im *InstancesModel) GetInstances() (instances []string) {
	im.mutex.Lock()
	instances = make([]string, len(im.instancesAlive))
	copy(instances, im.instancesAlive)
	im.mutex.Unlock()

	return
}

func (im *InstancesModel) delTable() {
	if tableDesc, err := im.conn.DescribeTable(im.tableName); err == nil {
		if _, err = im.conn.DeleteTable(*tableDesc); err != nil {
			log.Error("Can't remove Dynamo table:", im.tableName, "Error:", err)
		}
	} else {
		log.Error("Can't remove Dynamo table:", im.tableName, "Error:", err)
	}
}

func (im *InstancesModel) registerHostName(hostName string) {
	attribs := []dynamodb.Attribute{
		*dynamodb.NewStringAttribute(cPrimKey, hostName),
		*dynamodb.NewStringAttribute("ts", fmt.Sprintf("%d", time.Now().Unix())),
	}

	if _, err := im.table.PutItem(hostName, cPrimKey, attribs); err != nil {
		log.Fatal("The hostname can't be registered on the instances table, Error:", err)
	}
	log.Debug("Instance inserted:", hostName)
}

func (im *InstancesModel) updateInstances() {
	hostName, err := os.Hostname()
	if err != nil {
		log.Fatal("Can't get the hostname of the local machine:", err)
	}

	im.registerHostName(hostName)

	if rows, err := im.table.Scan(nil); err == nil {
		instances := []string{}
		for _, row := range rows {
			lastTs, _ := strconv.ParseInt(row["ts"].Value, 10, 64)
			if lastTs, _ = strconv.ParseInt(row["ts"].Value, 10, 64); lastTs+cTTL > time.Now().Unix() && row[cPrimKey].Value != hostName {
				instances = append(instances, row[cPrimKey].Value)
			}
		}
		im.mutex.Lock()
		im.instancesAlive = instances
		im.mutex.Unlock()
	} else {
		log.Error("Problem trying to get the list of instances from Dynamo DB, Error:", err)
	}
}

func (im *InstancesModel) initTable() {
	pKey := dynamodb.PrimaryKey{dynamodb.NewStringAttribute(cPrimKey, ""), nil}
	im.table = im.conn.NewTable(im.tableName, pKey)

	res, err := im.table.DescribeTable()
	if err != nil {
		log.Info("Creating a new table on DynamoDB:", im.tableName)
		td := dynamodb.TableDescriptionT{
			TableName: im.tableName,
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

		if _, err := im.conn.CreateTable(td); err != nil {
			log.Error("Error trying to create a table on Dynamo DB, table:", im.tableName, "Error:", err)
		}
		if res, err = im.table.DescribeTable(); err != nil {
			log.Error("Error trying to describe a table on Dynamo DB, table:", im.tableName, "Error:", err)
		}
	}
	for "ACTIVE" != res.TableStatus {
		if res, err = im.table.DescribeTable(); err != nil {
			log.Error("Can't describe Dynamo DB instances table, Error:", err)
		}
		log.Debug("Waiting for active table, current status:", res.TableStatus)
		time.Sleep(time.Second)
	}
}

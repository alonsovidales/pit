package users

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/alonsovidales/pit/cfg"
	"github.com/alonsovidales/pit/log"
	"github.com/goamz/goamz/aws"
	"github.com/goamz/goamz/dynamodb"
	"golang.org/x/crypto/pbkdf2"
	"os"
	"strings"
	"sync"
	"time"
)

const (
	cTable             = "users"
	cPrimKey           = "uid"
	cDefaultWRCapacity = 5
	cCacheTTL          = 10 * time.Second

	CActivityAccountType = "account"
	CActivityShardsType  = "shards"
)

type ModelInt interface {
	RegisterUserPlainKey(uid string, key string, ip string) (*User, error)
	HashPassword(password string) string
	RegisterUser(uid string, key string, ip string) (user *User, err error)
	GetUserInfo(uid string, key string) (user *User)
	AdminGetUserInfoByID(uid string) (user *User)
	GetRegisteredUsers() (users map[string]*User)
}

type Int interface {
	DisableUser() (persisted bool)
	EnableUser() (persisted bool)
	UpdateUser(key string) bool
	AddActivityLog(actionType string, des string, ip string) bool
	GetAllActivity() (activity map[string]*LogLine)
}

type LogLine struct {
	Ts      int64  `json:"ts"`
	IP      string `json:"ip"`
	LogType string `json:"type"`
	Desc    string `json:"desc"`
}

type Model struct {
	prefix    string
	secret    []byte
	tableName string
	conn      *dynamodb.Server
	table     *dynamodb.Table
	cache     map[string]*User
	mutex     sync.Mutex
}

type Billing struct {
	Inst map[string]int
	Ts   int64
}

type BillingLine struct {
	Group string `json:"group"`
	Instances int `json:"instances"`
	Type string `json:"type"`
	Price float64 `json:"price"`
	From int64 `json:"from"`
	To int64 `json:"to"`
	Paid bool `json:"paid"`
}

type User struct {
	Int `json:"-"`

	uid      string
	key      string
	Enabled  string `json:"-"`
	logs     map[string][]*LogLine
	billHist []*Billing

	RegTs int64  `json:"reg_ts"`
	RegIP string `json:"reg_ip"`

	mutex sync.Mutex
	md    *Model
}

type Bills struct {
	From    uint64  `json:"from"`
	To      uint64  `json:"to"`
	Amount float64 `json:"amount"`
	Paid bool `json:"paid"`
}

type BillingInfo struct {
	ToPay     float64      `json:"to_pay"`
	Bills []*Bills `json:"bills"`
	History   []*BillingLine   `json:"history"`
}

func GetModel(prefix string, awsRegion string) (um *Model) {
	if awsAuth, err := aws.EnvAuth(); err == nil {
		um = &Model{
			prefix:    prefix,
			tableName: fmt.Sprintf("%s_%s", prefix, cTable),
			secret:    []byte(os.Getenv("PIT_SECRET")),
			cache:     make(map[string]*User),
			conn: &dynamodb.Server{
				Auth:   awsAuth,
				Region: aws.Regions[awsRegion],
			},
		}
		um.initTable()

		go um.cacheManager()
	} else {
		log.Error("Problem trying to connect with DynamoDB, Error:", err)
	}

	return
}

func (um *Model) cacheManager() {
	c := time.Tick(cCacheTTL)
	for _ = range c {
		um.cache = make(map[string]*User)
	}
}

func (um *Model) RegisterUserPlainKey(uid string, key string, ip string) (*User, error) {
	// Sanitize e-mail addr removin all the + Chars in order to avoid fake
	// duplicated accounts
	uid = strings.Replace(uid, "+", "", -1)

	if um.AdminGetUserInfoByID(uid) != nil {
		return nil, errors.New("Existing user account")
	}

	user := &User{
		uid:      uid,
		key:      key,
		Enabled:  "1",
		logs:     make(map[string][]*LogLine),
		billHist: []*Billing{},

		RegTs: time.Now().Unix(),
		RegIP: ip,

		md: um,
	}

	if !user.persist() {
		return nil, errors.New("Error trying to store the user data")
	}
	return user, nil
}

func (um *Model) RegisterUser(uid string, key string, ip string) (*User, error) {
	return um.RegisterUserPlainKey(uid, um.HashPassword(key), ip)
}

func (um *Model) GetUserInfo(uid string, key string) (user *User) {
	user = um.AdminGetUserInfoByID(uid)
	if user == nil || user.key != um.HashPassword(key) || user.Enabled == "0" {
		return nil
	}

	return
}

func (um *Model) AdminGetUserInfoByID(uid string) (user *User) {
	um.mutex.Lock()
	defer um.mutex.Unlock()
	if us, ok := um.cache[uid]; ok {
		return us
	}

	attKey := &dynamodb.Key{
		HashKey:  uid,
		RangeKey: "",
	}

	if data, err := um.table.GetItemConsistent(attKey, false); err == nil {
		user = &User{
			uid:      uid,
			key:      data["key"].Value,
			Enabled:  data["enabled"].Value,
			logs:     make(map[string][]*LogLine),
			billHist: []*Billing{},
			md:       um,
		}
		if err := json.Unmarshal([]byte(data["info"].Value), &user); err != nil {
			log.Error("Problem trying to retieve the user information for user:", uid, "Error:", err)
			return nil
		}
		if _, ok := data["logs"]; ok {
			if err = json.Unmarshal([]byte(data["logs"].Value), &user.logs); err != nil {
				log.Error("Problem trying to unmarshal the user logs for user:", uid, "Error:", err)
			}
		}
		if _, ok := data["bill_hist"]; ok {
			if err = json.Unmarshal([]byte(data["bill_hist"].Value), &user.billHist); err != nil {
				log.Error("Problem trying to unmarshal the user billing history for user:", uid, "Error:", err)
			}
		}
	} else {
		log.Error("Problem trying to read the user information for user:", uid, "Error:", err)
	}

	um.cache[uid] = user
	return
}

func (um *Model) GetRegisteredUsers() (users map[string]*User) {
	if rows, err := um.table.Scan(nil); err == nil {
		users = make(map[string]*User)
		for _, row := range rows {
			uid := row["uid"].Value
			user := &User{
				uid:      uid,
				key:      row["key"].Value,
				Enabled:  row["enabled"].Value,
				logs:     make(map[string][]*LogLine),
				billHist: []*Billing{},
				md:       um,
			}
			if err := json.Unmarshal([]byte(row["info"].Value), &user); err != nil {
				log.Error("Problem trying to retieve the user information for user:", user.uid, "Error:", err)
				return nil
			}
			if err = json.Unmarshal([]byte(row["logs"].Value), &user.logs); err != nil {
				log.Error("Problem trying to unmarshal the user logs for user:", user.uid, "Error:", err)
				return nil
			}
			if err = json.Unmarshal([]byte(row["bill_hist"].Value), &user.billHist); err != nil {
				log.Error("Problem trying to unmarshal the billing history for user:", user.uid, "Error:", err)
				return nil
			}
			users[uid] = user
		}
	}

	return
}

func (us *User) GetBillingInfo() (bi *BillingInfo) {
/*type Billing struct {
	Inst map[string]int
	Ts   int64
}*/
/*type BillingLine struct {
	Group string `json:"group"`
	Instances int `json:"instances"`
	Type string `json:"type"`
	Price float64 `json:"price"`
	From int64 `json:"from"`
	To int64 `json:"to"`*/
	bi = &BillingInfo{
		ToPay: 0.0,
		Bills: []*Bills{
			&Bills{
				From:    1432767388,
				To:      1432807388,
				Amount: 293.5,
				Paid: true,
			},
			&Bills{
				From:    1432807388,
				To:      1432836873,
				Amount: 403.5,
				Paid: true,
			},
			&Bills{
				From:    1432836873,
				To:      1433345395,
				Amount: 800.45,
				Paid: false,
			},
		},
		History: []*BillingLine{},
	}

	lastBillTs := int64(bi.Bills[len(bi.Bills)-1].To)
	lastTimeSaw := make(map[string]int64)
	lastNumInst := make(map[string]int)
	groupsCovered := make(map[string]bool)

	for _, bl := range us.billHist {
		for group, instances := range bl.Inst {
			toTs := bl.Ts
			billingBorder := false
			if _, ok := groupsCovered[group]; !ok && toTs > lastBillTs {
				toTs = lastBillTs
				billingBorder = true
				groupsCovered[group] = true
			}

			if lastInst, ok := lastNumInst[group]; ok {
				if lastInst > 1 {
					lastInst--
				}
				if instances != lastInst || billingBorder {
					if lastInst > 0 {
						parts := strings.SplitAfterN(group, ":", 2)
						groupType := parts[0][:len(parts[0])-1]
						_, _, costHour := GetGroupInfo(groupType)
						totalTime := toTs - lastTimeSaw[group]
						cost := costHour * (float64(totalTime) / 3600) * float64(lastInst)
						bi.History = append(bi.History, &BillingLine{
							Group: parts[1],
							Instances: lastInst,
							Type: groupType,
							Price: cost,
							From: lastTimeSaw[group],
							To: toTs,
							Paid: toTs <= lastBillTs,
						})

						if toTs > lastBillTs {
							bi.ToPay += cost
						}
					}

					lastTimeSaw[group] = toTs
					lastNumInst[group] = instances
				}
			} else {
				lastTimeSaw[group] = toTs
				lastNumInst[group] = instances
			}
		}

		for group, _ := range lastNumInst {
			if _, ok := bl.Inst[group]; !ok {
				delete(lastTimeSaw, group)
				delete(lastNumInst, group)
			}
		}

	}

	for group, instances := range lastNumInst {
		if instances > 1 {
			instances--
		}
		if instances > 0 {
			parts := strings.SplitAfterN(group, ":", 2)
			groupType := parts[0][:len(parts[0])-1]
			totalTime := time.Now().Unix() - lastTimeSaw[group]
			_, _, costHour := GetGroupInfo(groupType)
			cost := costHour * (float64(totalTime) / 3600) * float64(instances)

			bi.History = append(bi.History, &BillingLine {
				Group: parts[1],
				Instances: instances,
				Type: groupType,
				Price: cost,
				From: lastTimeSaw[group],
				To: time.Now().Unix(),
				Paid: false,
			})
			bi.ToPay += cost
		}
	}

	return
}

func (us *User) DisableUser() (persisted bool) {
	us.Enabled = "0"

	return us.persist()
}

func (us *User) EnableUser() (persisted bool) {
	us.Enabled = "1"

	return us.persist()
}

func (us *User) UpdateUser(key string) bool {
	us.key = us.md.HashPassword(key)

	return us.persist()
}

func (us *User) GetLastBillInfo() *Billing {
	if len(us.billHist) == 0 {
		return nil
	}
	return us.billHist[len(us.billHist)-1]
}

func (us *User) AddBillingHist(instruments map[string]int) bool {
	us.billHist = append(us.billHist, &Billing{
		Inst: instruments,
		Ts:   time.Now().Unix(),
	})

	return us.persist()
}

func (us *User) AddActivityLog(actionType string, desc, ip string) bool {
	if _, ok := us.logs[actionType]; !ok {
		us.logs[actionType] = []*LogLine{}
	}

	us.logs[actionType] = append(us.logs[actionType], &LogLine{
		IP:      ip,
		Ts:      time.Now().Unix(),
		LogType: actionType,
		Desc:    desc,
	})

	return us.persist()
}

func (us *User) GetAllActivity() (activity map[string][]*LogLine) {
	return us.logs
}

func (um *Model) HashPassword(password string) string {
	return base64.StdEncoding.EncodeToString(pbkdf2.Key([]byte(password), um.secret, 4096, sha256.Size, sha256.New))
}

func (um *Model) delTable() {
	if tableDesc, err := um.conn.DescribeTable(um.tableName); err == nil {
		if _, err = um.conn.DeleteTable(*tableDesc); err != nil {
			log.Error("Can't remove Dynamo table:", um.tableName, "Error:", err)
		}
	} else {
		log.Error("Can't remove Dynamo table:", um.tableName, "Error:", err)
	}
}

func (us *User) persist() bool {
	userJSONInfo, _ := json.Marshal(us)
	userJSONLogs, _ := json.Marshal(us.logs)
	userJSONBillHist, _ := json.Marshal(us.billHist)

	attribs := []dynamodb.Attribute{
		*dynamodb.NewStringAttribute(cPrimKey, us.uid),
		*dynamodb.NewStringAttribute("key", us.key),
		*dynamodb.NewStringAttribute("info", string(userJSONInfo)),
		*dynamodb.NewStringAttribute("bill_hist", string(userJSONBillHist)),
		*dynamodb.NewStringAttribute("logs", string(userJSONLogs)),
		*dynamodb.NewStringAttribute("enabled", string(us.Enabled)),
	}

	if _, err := us.md.table.PutItem(us.uid, cPrimKey, attribs); err != nil {
		log.Error("A new user can't be registered on the users table, Error:", err)

		return false
	}

	return true
}

func (um *Model) initTable() {
	pKey := dynamodb.PrimaryKey{dynamodb.NewStringAttribute(cPrimKey, ""), nil}
	um.table = um.conn.NewTable(um.tableName, pKey)

	res, err := um.table.DescribeTable()
	if err != nil {
		log.Info("Creating a new table on DynamoDB:", um.tableName)
		td := dynamodb.TableDescriptionT{
			TableName: um.tableName,
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

		if _, err := um.conn.CreateTable(td); err != nil {
			log.Error("Error trying to create a table on Dynamo DB, table:", um.tableName, "Error:", err)
		}
		if res, err = um.table.DescribeTable(); err != nil {
			log.Error("Error trying to describe a table on Dynamo DB, table:", um.tableName, "Error:", err)
		}
	}
	for "ACTIVE" != res.TableStatus {
		if res, err = um.table.DescribeTable(); err != nil {
			log.Error("Can't describe Dynamo DB instances table, Error:", err)
		}
		log.Debug("Waiting for active table, current status:", res.TableStatus)
		time.Sleep(time.Second)
	}
}

func GetGroupInfo(groupType string) (reqs, records uint64, costHour float64) {
	switch groupType {
	case "s":
		reqs = cfg.GetUint64("group-types", "small-reqs")
		records = cfg.GetUint64("group-types", "small-records")
		costHour = cfg.GetFloat("group-types", "small-cost-hour")
	case "m":
		reqs = cfg.GetUint64("group-types", "medium-reqs")
		records = cfg.GetUint64("group-types", "medium-records")
		costHour = cfg.GetFloat("group-types", "medium-cost-hour")
	case "l":
		reqs = cfg.GetUint64("group-types", "large-reqs")
		records = cfg.GetUint64("group-types", "large-records")
		costHour = cfg.GetFloat("group-types", "large-cost-hour")
	case "xl":
		reqs = cfg.GetUint64("group-types", "x-large-reqs")
		records = cfg.GetUint64("group-types", "x-large-records")
		costHour = cfg.GetFloat("group-types", "x-large-cost-hour")
	case "xxl":
		reqs = cfg.GetUint64("group-types", "xx-large-reqs")
		records = cfg.GetUint64("group-types", "xx-large-records")
		costHour = cfg.GetFloat("group-types", "xx-large-cost-hour")
	}

	return
}

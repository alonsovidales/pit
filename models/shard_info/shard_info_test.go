package shardinfo

import (
	//"github.com/alonsovidales/pit/log"
	//"github.com/alonsovidales/pit/models/instances"
	"os"
	"reflect"
	"testing"
	"time"
)

var md *Model

func TestMain(m *testing.M) {
	md = GetModel("test", "eu-west-1")
	time.Sleep(time.Second * 20)

	retCode := m.Run()

	md.delTables()

	os.Exit(retCode)
}

func TestAddGroupPersistAndRead(t *testing.T) {
	var err error

	originalGroups := make([]*GroupInfo, 3)
	originalGroups[0], err = md.AddGroup("userID", "secret", "groupId", 1, 1000000, 100, 1000, 5)
	if err != nil {
		t.Error("Problem trying to insert a new group, Error:", err)
		t.Fail()
	}

	originalGroups[1], err = md.AddGroup("userID1", "secret1", "groupId1", 2, 1000400, 110, 160, 6)
	if err != nil {
		t.Error("Problem trying to insert a new group, Error:", err)
		t.Fail()
	}

	originalGroups[2], err = md.AddGroup("userID2", "secret2", "groupId2", 3, 100000, 300, 1500, 10)
	if err != nil {
		t.Error("Problem trying to insert a new group, Error:", err)
		t.Fail()
	}

	time.Sleep(time.Second * (cUpdatePeriod * 2))

	grById, err := md.GetGroupByUserKeyId("dljvnekw", "secret", "123")
	if err != CErrGroupUserNotFound || grById != nil {
		t.Error("Trying to get a group for an unexisting user, but the system didn't return the corresponding error, error returned:", err)
	}

	grById, err = md.GetGroupByUserKeyId("userID", "secret", "123")
	if err != CErrGroupNotFound || grById != nil {
		t.Error("Trying to get a unexisting group, but the system didn't return the corresponding error, error returned:", err)
	}

	grById, err = md.GetGroupByUserKeyId("userID", "asd", "groupId")
	if err != CErrAuth || grById != nil {
		t.Error("Trying to get a group using unvalid credentials, but the system didn't return the corresponding error, error returned:", err)
	}

	_, err = md.AddGroup("userID", "secret", "groupId", 3, 1000000, 100, 1000, 10)
	if err != CErrGroupInUse {
		t.Error("Trying to add a duplicated group ID, but the system didn't return the corresponding error, error returned:", err)
	}

	for _, gr := range originalGroups {
		grToCompare, err := md.GetGroupByUserKeyId(gr.UserID, gr.Secret, gr.GroupID)
		if err != nil || grToCompare == nil {
			t.Error("The group can't be obtained from the model, Error:", err)
			t.Fail()
		}

		for k := range gr.Shards {
			if _, ok := grToCompare.Shards[k]; !ok || !reflect.DeepEqual(gr.Shards[k], grToCompare.Shards[k]) {
				t.Error("After store and read a group, the shards contained are not equal:", gr.Shards[k], grToCompare.Shards[k])
			}
		}

		gr.md = nil
		grToCompare.md = nil
		gr.Shards = nil
		grToCompare.Shards = nil

		if gr.UserID != grToCompare.UserID ||
			gr.Secret != grToCompare.Secret ||
			gr.GroupID != grToCompare.GroupID ||
			gr.MaxScore != grToCompare.MaxScore ||
			gr.NumShards != grToCompare.NumShards ||
			gr.MaxReqSec != grToCompare.MaxReqSec ||
			gr.MaxInsertReqSec != grToCompare.MaxInsertReqSec ||
			gr.MaxElements != grToCompare.MaxElements {
			t.Error("After store and read a group, the result is not equal to the inserted group", gr, grToCompare)
		}
	}
}

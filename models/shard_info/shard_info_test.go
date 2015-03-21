package shardinfo

import (
	"github.com/alonsovidales/pit/log"
	"github.com/alonsovidales/pit/models/instances"
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

	md.delTable()

	os.Exit(retCode)
}

func TestAddGroupPersistAndRead(t *testing.T) {
	var err error

	originalGroups := make([]*GroupInfo, 3)
	originalGroups[0], err = md.AddGroup("userId", "secret", "groupId", 1, 1000000, 100, 1000, 5)
	if err != nil {
		t.Error("Problem trying to insert a new group, Error:", err)
		t.Fail()
	}

	originalGroups[1], err = md.AddGroup("userId1", "secret1", "groupId1", 2, 1000400, 110, 160, 6)
	if err != nil {
		t.Error("Problem trying to insert a new group, Error:", err)
		t.Fail()
	}

	originalGroups[2], err = md.AddGroup("userId2", "secret2", "groupId2", 3, 100000, 300, 1500, 10)
	if err != nil {
		t.Error("Problem trying to insert a new group, Error:", err)
		t.Fail()
	}

	time.Sleep(time.Second * (cUpdatePeriod * 2))

	grById, err := md.GetGroupById("dljvnekw", "secret", "123")
	if err != CErrGroupUserNotFound || grById != nil {
		t.Error("Trying to get a group for an unexisting user, but the system didn't return the corresponding error, error returned:", err)
	}

	grById, err = md.GetGroupById("userId", "secret", "123")
	if err != CErrGroupNotFound || grById != nil {
		t.Error("Trying to get a unexisting group, but the system didn't return the corresponding error, error returned:", err)
	}

	grById, err = md.GetGroupById("userId", "asd", "groupId")
	if err != CErrAuth || grById != nil {
		t.Error("Trying to get a group using unvalid credentials, but the system didn't return the corresponding error, error returned:", err)
	}

	_, err = md.AddGroup("userId", "secret", "groupId", 3, 1000000, 100, 1000, 10)
	if err != CErrGroupInUse {
		t.Error("Trying to add a duplicated group ID, but the system didn't return the corresponding error, error returned:", err)
	}

	for _, gr := range originalGroups {
		grToCompare, err := md.GetGroupById(gr.UserId, gr.Secret, gr.GroupId)
		if err != nil || grToCompare == nil {
			t.Error("The group can't be obtained from the model, Error:", err)
			t.Fail()
		}

		if !reflect.DeepEqual(gr, grToCompare) {
			t.Error("After store and read a group, the result is not equal to the inserted group", gr, grToCompare)
		}
	}
}

func TestAcquireShard(t *testing.T) {
	gr, err := md.AddGroup("userId2", "secret2", "groupId22", 2, 100000, 300, 1500, 3)
	log.Debug("Group:", gr)
	if err != nil {
		t.Error("Problem trying to insert a new group, Error:", err)
		t.Fail()
	}

	log.Debug("Group1:", gr)
	adquired, err := gr.AcquireShard()
	if !adquired || err != nil {
		t.Error("The system couldn't acquire a shard in a group with a limit of 2 and no shards in use, Error:", err)
	}

	log.Debug("Group2:", gr)
	adquired, err = gr.AcquireShard()
	if adquired || err != CSharPrevOwnedGroup {
		t.Error("The system could acquire a shard in a group where we owned one of the shards before, Error:", err)
	}

	instances.SetHostname("testHn")

	adquired, err = gr.AcquireShard()
	if !adquired || err != nil {
		t.Error("The system couldn't acquire a shard in a group where still is a shard free, Error:", err)
	}

	md.ReleaseAllAdquiredShards()

	adquired, err = gr.AcquireShard()
	if !adquired || err != nil {
		t.Error("The system couldn't acquire a shard in a group where still is a shard free because it was previously released, Error:", err)
	}

	instances.SetHostname("testHn1")

	adquired, err = gr.AcquireShard()
	if adquired || err != CErrMaxShardsByGroup {
		t.Error("The system could acquire a shard in a full group, Error:", err)
	}
}

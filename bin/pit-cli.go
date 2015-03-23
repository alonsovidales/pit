package main

import (
	"os"
	"fmt"
	"strings"
	"github.com/alonsovidales/pit/cfg"
	"gopkg.in/alecthomas/kingpin.v1"
	"github.com/alonsovidales/pit/models/shard_info"
	"github.com/alonsovidales/pit/models/instances"
	"text/tabwriter"
)

// Terminal text colours
const (
	// CLR0 Default terminal colour
	CLR0 = "\x1b[30;1m"
	// CLRR Red colour
	CLRR = "\x1b[31;1m"
	// CLRG Green colour
	CLRG = "\x1b[32;1m"
	// CLRY Yellow colour
	CLRY = "\x1b[33;1m"
	// CLRB Blue colour
	CLRB = "\x1b[34;1m"
	// CLRN Neutral colour
	CLRN = "\x1b[0m"
)

func main() {
	app := kingpin.New("pit-cli", "Pit-cli is a tool to manage all the different features of the recommener system Pit")

	env := app.Flag("env", "Configuration environment: pro, pre, dev").Required().Enum("pro", "pre", "dev")

	cmdInstances := app.Command("instances", "Manage all the instances of the cluster")
	cmdInstancesList := cmdInstances.Command("list", "Lists all the instances on the clusted")

	cmdGroups := app.Command("groups", "Manage all the recommendation groups allocated by Pit")
	cmdGroupsList := cmdGroups.Command("list", "lists all the shards in the system")
	cmdGroupsListUser := cmdGroupsList.Flag("userid", `List the instances for this user`).String()

	cmdGroupsDel := cmdGroups.Command("del", "Removes one of the groups")
	cmdGroupsDelGroupId := cmdGroupsDel.Arg("group-id", `ID of the group to be removed`).Required().String()

	cmdGroupsAdd := cmdGroups.Command("update", "Adds or updates an existing shard")
	cmdGroupsAddMaxScore := cmdGroupsAdd.Flag("max-score", `Max possible score`).Required().Int()
	cmdGroupsAddNumShards := cmdGroupsAdd.Flag("num-shards", `Total number of shards`).Required().Int()
	cmdGroupsAddMaxElements := cmdGroupsAdd.Flag("num-elems", `Max number of elements that can be allocated by shard`).Required().Int()
	cmdGroupsAddMaxReqSec := cmdGroupsAdd.Flag("max-req-sec", `Max number of requests by second`).Required().Int()
	cmdGroupsAddMaxInsertReqSec := cmdGroupsAdd.Flag("max-ins-req-sec", `Max number of insert requests`).Required().Int()
	cmdGroupsAddUserID := cmdGroupsAdd.Flag("user-id", `User ID of the owner of this group`).Required().String()
	cmdGroupsAddKey := cmdGroupsAdd.Flag("key", `Access key token of this group`).Required().String()
	cmdGroupsAddGroupID := cmdGroupsAdd.Flag("group-id", `ID of the group to be updated or added`).Required().String()

	kingpin.MustParse(app.Parse(os.Args[1:]))
	cfg.Init("pit", *env)
	switch kingpin.MustParse(app.Parse(os.Args[1:])) {
	case cmdInstancesList.FullCommand():
		listInstances()

	case cmdGroupsList.FullCommand():
		listGroups(*cmdGroupsListUser)

	case cmdGroupsDel.FullCommand():
		delGroup(*cmdGroupsDelGroupId)

	case cmdGroupsAdd.FullCommand():
		addGroup(
			*cmdGroupsAddUserID,
			*cmdGroupsAddKey,
			*cmdGroupsAddGroupID,
			*cmdGroupsAddNumShards,
			uint64(*cmdGroupsAddMaxElements),
			uint64(*cmdGroupsAddMaxReqSec),
			uint64(*cmdGroupsAddMaxInsertReqSec),
			uint8(*cmdGroupsAddMaxScore))

	default:
		fmt.Printf("Not command specified, use: \"%s --help\" to get help\n", strings.Join(os.Args, " "))
	}
}

func listInstances() {
	md := instances.InitAndKeepAlive(
		cfg.GetStr("aws", "prefix"),
		cfg.GetStr("aws", "region"),
		false)

	fmt.Println(CLRG + "Instances" + CLRN)
	fmt.Println(CLRG + "---------" + CLRN)
	for _, instance := range md.GetInstances() {
		fmt.Println(instance)
	}
}

func listGroups(userId string) {
	md := shardinfo.GetModel(
		cfg.GetStr("aws", "prefix"),
		cfg.GetStr("aws", "region"))

	w := new(tabwriter.Writer)
	w.Init(os.Stdout, 0, 8, 3, '\t', 0)
	fmt.Fprintln(w, "User ID\tSecret\tGroupId\tMax Score\tTotal Shards\tMax Elements\tMax req sec\tMax Insert Req Sec\tShard owners")
	fmt.Fprintln(w, "-------\t------\t-------\t---------\t------------\t------------\t-----------\t------------------\t------------")


	groups := md.GetAllGroups()
	for _, groups := range groups {
		for _, group := range groups {
			shardOwners := ""
			for _, shard := range group.Shards {
				shardOwners += fmt.Sprintf("%s %d %d\t", shard.Addr, shard.LastTs, shard.ReqSec)
			}

			fmt.Fprintf(
				w,
				"%s\t%s\t%s\t%d\t%d\t%d\t%d\t%d\t%s\n",
				group.UserId,
				group.Secret,
				group.GroupId,
				group.MaxScore,
				group.NumShards,
				group.MaxElements,
				group.MaxReqSec,
				group.MaxInsertReqSec,
				shardOwners)
		}
	}
	w.Flush()
}

func delGroup(groupId string) {
	fmt.Println("The next group will be deleted:")
	md := shardinfo.GetModel(
		cfg.GetStr("aws", "prefix"),
		cfg.GetStr("aws", "region"))

	group := md.GetGroupById(groupId)
	if group == nil {
		fmt.Println("Group not found with ID:", groupId)
		return
	}

	if askForConfirmation() {
		md.RemoveGroup(groupId)
		fmt.Println("Group removed")
	}
}

func addGroup(userId, secret, groupId string, numShards int, maxElements, maxReqSec, maxInsertReqSec uint64, maxScore uint8) {
	md := shardinfo.GetModel(
		cfg.GetStr("aws", "prefix"),
		cfg.GetStr("aws", "region"))

	fmt.Println(CLRG + "The next group will be added:" + CLRN)
	fmt.Println("User ID:", userId)
	fmt.Println("Secret:", secret)
	fmt.Println("Group ID:", groupId)
	fmt.Println("Num Shards:", numShards)
	fmt.Println("Max elements:", maxElements)
	fmt.Println("Max requests by sec / shard:", maxReqSec)
	fmt.Println("Max Insert requests by sec / shard:", maxInsertReqSec)
	fmt.Println("Max score:", maxScore)

	if askForConfirmation() {
		_, err := md.AddGroup(userId, secret, groupId, numShards, maxElements, maxReqSec, maxInsertReqSec, maxScore)
		if err != nil {
			fmt.Println("Problem adding a new group, Error:", err)
		}
	}
}

func askForConfirmation() bool {
	var response string

	fmt.Print(CLRR + "\nAre you completly sure? (y/n): " + CLRN)
	if _, err := fmt.Scanln(&response); err != nil {
		return false
	}

	possibleAnsw := map[string]bool{
		"y":   true,
		"Y":   true,
		"yes": true,
		"Yes": true,
		"YES": true,
	}

	_, ok := possibleAnsw[response]

	return ok
}

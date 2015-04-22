package recommender

import (
	"bufio"
	"encoding/json"
	"github.com/alonsovidales/pit/log"
	"os"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
)

const (
	TESTSET = "../test_training_set/training_set.info"
)

func TestCompression(t *testing.T) {
	aux := "This is a test..."
	rc := &Recommender{}
	if string(rc.uncompress(rc.compress([]byte(aux)))) != aux {
		t.Error("Problem trying to compress and uncompress data")
	}
}

func TestRecommenderLoadNoBackup(t *testing.T) {
	sh := NewShard("/testing", "test_collab_insertion_no_baackup", 10, 5, "eu-west-1")
	if sh.LoadBackup() {
		t.Error("The method LoadBackup can't return true when a backup doesn't exist")
	}
}

func TestRecommenderSaveLoad(t *testing.T) {
	maxClassifications := uint64(1000000)
	runtime.GOMAXPROCS(runtime.NumCPU())

	sh := NewShard("/testing", "test_collab_insertion", maxClassifications, 5, "eu-west-1")

	f, err := os.Open(TESTSET)
	if err != nil {
		log.Error("Can't read the the test set file:", TESTSET, "Error:", err)
		t.Fail()
	}
	r := bufio.NewReader(f)
	s, e := Readln(r)
	i := 0
	for e == nil && i < 100000 {
		s, e = Readln(r)
		recID, scores := parseLine(s)
		sh.AddRecord(recID, scores)
		i++
		if i%1000 == 0 {
			log.Debug("Lines processed:", i)
		}
	}

	time.Sleep(time.Second)

	if sh.totalClassif > maxClassifications {
		t.Error(
			"Problem with the garbage collection, the total number of stored stores are:",
			sh.totalClassif, "and the max defined boundary is:", maxClassifications)
	}

	if sh.status != STATUS_SARTING {
		t.Error("The expectede status was:", STATUS_SARTING, "but the actual one is:", sh.status)
	}

	log.Debug("Processing tree...")
	sh.RecalculateTree()

	if sh.status != STATUS_ACTIVE {
		t.Error("The expectede status was:", STATUS_ACTIVE, "but the actual one is:", sh.status)
	}

	s, e = Readln(r)
	recID, scores := parseLine(s)
	recomendationsBef := sh.CalcScores(recID, scores, 10)
	if len(recomendationsBef) != 10 {
		t.Error("The expected recommendations was 10, but:", len(recomendationsBef), "obtained.")
	}

	prevScores := sh.totalClassif
	sh.SaveBackup()

	sh = NewShard("/testing", "test_collab_insertion", maxClassifications, 5, "eu-west-1")
	sh.RecalculateTree()

	if sh.status != STATUS_NORECORDS {
		t.Error("The expectede status was:", STATUS_NORECORDS, "but the actual one is:", sh.status)
	}

	sh.LoadBackup()

	sh.RecalculateTree()

	if prevScores != sh.totalClassif {
		t.Error(
			"Before store a backup the number of records was:", prevScores,
			"but after load the backup is:", sh.totalClassif)
	}

	recomendationsAfter := sh.CalcScores(recID, scores, 10)
	if len(recomendationsAfter) != 10 {
		t.Error("The expected recommendations was 10, but:", len(recomendationsAfter), "obtained.")
	}

	log.Debug("Classifications:", sh.maxClassif)
}

func Readln(r *bufio.Reader) (string, error) {
	var err error
	var line, ln []byte

	isPrefix := true
	for isPrefix && err == nil {
		line, isPrefix, err = r.ReadLine()
		ln = append(ln, line...)
	}

	return string(ln), err
}

func parseLine(line string) (recordID uint64, values map[uint64]uint8) {
	parts := strings.SplitN(line, ":", 2)
	recordIDOrig, _ := strconv.ParseInt(parts[0], 10, 64)
	recordID = uint64(recordIDOrig)

	valuesAux := make(map[string]uint8)
	if len(parts) < 2 {
		log.Fatal(line)
	}
	json.Unmarshal([]byte(parts[1]), &valuesAux)
	values = make(map[uint64]uint8)
	for k, v := range valuesAux {
		kI, _ := strconv.ParseInt(k, 10, 64)
		values[uint64(kI)] = v
	}

	return
}

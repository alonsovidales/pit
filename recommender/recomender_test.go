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
	TESTSET = "training_set/training_set.info"
)

func TestCollabInsertion(t *testing.T) {
	runtime.GOMAXPROCS(runtime.NumCPU())
	maxMemMB := uint64(100)

	rc := Init(maxMemMB)

	f, err := os.Open(TESTSET)
	if err != nil {
		log.Error("Can't read the the test set file:", TESTSET, "Error:", err)
		t.Fail()
	}
	r := bufio.NewReader(f)
	s, e := Readln(r)
	i := 0
	for e == nil && i < 10000 {
		s, e = Readln(r)
		recId, scores := parseLine(s)
		rc.AddRecord(recId, scores)
		i++
	}

	time.Sleep(100 * time.Second)

	memStats := new(runtime.MemStats)
	runtime.ReadMemStats(memStats)
	if maxMemMB*1000000 < memStats.Alloc {
		t.Error("Garbage collector not working propertly, max memory setted to:", maxMemMB, "but:", memStats.Alloc, "bytes used")
	}
}

func Readln(r *bufio.Reader) (string, error) {
	var isPrefix bool = true
	var err error = nil
	var line, ln []byte
	for isPrefix && err == nil {
		line, isPrefix, err = r.ReadLine()
		ln = append(ln, line...)
	}

	return string(ln), err
}

func parseLine(line string) (recordId uint64, values map[uint64]float64) {
	parts := strings.SplitN(line, ":", 2)
	recordIdOrig, _ := strconv.ParseInt(parts[0], 10, 64)
	recordId = uint64(recordIdOrig)

	valuesAux := make(map[string]float64)
	if len(parts) < 2 {
		log.Fatal(line)
	}
	json.Unmarshal([]byte(parts[1]), &valuesAux)
	values = make(map[uint64]float64)
	for k, v := range valuesAux {
		kI, _ := strconv.ParseInt(k, 10, 64)
		values[uint64(kI)] = v
	}

	return
}

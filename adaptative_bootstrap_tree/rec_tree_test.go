package rectree

import (
	"os"
	"encoding/json"
	"strconv"
	"runtime"
	"bufio"
	"github.com/alonsovidales/pit/log"
	"testing"
	"strings"
)

const (
	TESTSET = "../test_training_set/training_set.info"
)

func TestCollabInsertion(t *testing.T) {
	runtime.GOMAXPROCS(runtime.NumCPU())

	f, err := os.Open(TESTSET)
	if err != nil {
		log.Error("Can't read the the test set file:", TESTSET, "Error:", err)
		t.Fail()
	}
	r := bufio.NewReader(f)
	s, e := Readln(r)
	records := []map[uint64]uint8{}
	log.Info("Parsing test file...")
	for i := 0; e == nil && i < 10000; i++ {
		s, e = Readln(r)
		_, scores := parseLine(s)
		records = append(records, scores)
	}
	log.Info("Generating tree...")
	tr := GetNewTree(records, 10, 5)
	log.Info("Tree generated...")
	tr.printTree()
}

func Readln(r *bufio.Reader) (string, error) {
	var isPrefix bool = true
	var err error = nil
	var line, ln []byte
	for isPrefix && err == nil {
		line, isPrefix, err = r.ReadLine()
		ln = append(ln, line...)
	}

	return string(ln),err
}

func parseLine(line string) (recordId uint64, values map[uint64]uint8) {
	parts := strings.SplitN(line, ":", 2)
	recordIdOrig, _ := strconv.ParseInt(parts[0], 10, 64)
	recordId = uint64(recordIdOrig)

	valuesAux := make(map[string]uint8)
	if len(parts) < 2 {
		log.Fatal(line)
	}
	json.Unmarshal([]byte(parts[1]), &valuesAux)
	values = make(map[uint64]uint8)
	for k, v := range valuesAux {
		kI, _ := strconv.ParseInt(k, 10, 64)
		values[uint64(kI)] = uint8(v)
	}

	return
}

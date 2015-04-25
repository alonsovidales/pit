package rectree

import (
	"bufio"
	"encoding/json"
	"github.com/alonsovidales/pit/log"
	"math"
	"os"
	"runtime"
	"strconv"
	"strings"
	"testing"
)

const (
	TESTSET  = "../test_training_set/training_set.info"
	MAXSCORE = 5
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
	//for i := 0; e == nil && i < 480187; i++ {
		s, e = Readln(r)
		_, scores := parseLine(s)
		records = append(records, scores)
	}
	log.Info("Generating tree...")
	tr, _ := ProcessNewTrees(records, 50, MAXSCORE, 3)
	tr.setTestMode()
	log.Info("Tree generated...")

	quadError := 0.0
	comparedItems := 0
	for i := 0; e == nil && i < 1000; i++ {
		s, e = Readln(r)
		_, scores := parseLine(s)
		elements := tr.GetBestRecommendation(scores, 10)

		for _, elemID := range elements {
			if score, rated := scores[elemID]; rated {
				quadError += (1.0 - (float64(score) / MAXSCORE)) * (1.0 - (float64(score) / MAXSCORE))
				comparedItems++
			}
		}
	}

	// Estimate the Root-mean-square deviation, we will use 0.3 for this test because the training set and the number of trees is too low
	rmsd := math.Sqrt(quadError / float64(comparedItems))
	if rmsd > 0.3 {
		t.Error("The RMSD is bigger than the expected, obtained:", rmsd)
	}

	return
}

func Readln(r *bufio.Reader) (string, error) {
	var isPrefix = true
	var err error
	var line, ln []byte
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
		values[uint64(kI)] = uint8(v)
	}

	return
}

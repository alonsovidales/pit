package recommender

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"fmt"
	"github.com/alonsovidales/pit/adaptative_bootstrap_tree"
	"github.com/alonsovidales/pit/log"
	"github.com/goamz/goamz/aws"
	"github.com/goamz/goamz/s3"
	"io/ioutil"
	"strings"
	"sync"
	"time"
)

const (
	// StatusLoading The shard is loading data from the storage
	StatusLoading = "LOADING"
	// StatusStarting After load the data, the shard is recalculating the tree
	StatusStarting = "STARTING"
	// StatusActive The shard is ready to perform prefictions with enought
	// data loaded in memory and the tree builded
	StatusActive = "ACTIVE"
	// StatusNoRecords There is not enought records in memory to start the
	// tree calculations, see cMinRecordsToStart
	StatusNoRecords = "NO_RECORDS"

	// cMinRecordsToStart The minimal number of records to build a tree
	cMinRecordsToStart = 100
	// cRecTreeMaxDeep The max deep for the tree, as higest the deep, as
	// better are going to be the predictions, but the bias will be higer
	// also be higer as also the time to recalculate the tree
	cRecTreeMaxDeep = 30
	// cRecTreeNumOfTrees The number of root threes to have for this shard,
	// the root trees are going to be the trees that starts for the most
	// common items
	cRecTreeNumOfTrees = 10

	// S3BUCKET name of the S3 bucket where the backups are going to be stored
	S3BUCKET = "pit-backups"
)

// Int Interface the defined all the possible interactions with this
// recommender system
type Int interface {
	// CalcScores Calculates the scores for the given records, and stores
	// in memory the classification for further processing
	CalcScores(recID uint64, scores map[uint64]uint8, maxToReturn int) (result []uint64)
	// AddRecord Just adds a new record to the recommender system in order
	// to increase the knoledge DB
	AddRecord(recID uint64, scores map[uint64]uint8)
	// GetTotalElements Returns the max number of elements that can ba
	// allocated on this recomender shard
	GetTotalElements() uint64
	// RecalculateTree Lanches the ETL process to create the tree
	RecalculateTree()
	// SaveBackup Stores all the records serialized in a inexpensive
	// storage system
	SaveBackup()
	// LoadBackup Restores all the information from backup
	LoadBackup() (success bool)
	// GetStatus Returns the current status of this recommender system,
	// the posible statuses can be: LOADING, ACTIVE, STARTING, NO_RECORDS
	GetStatus() string
	// GetStoredElements Returns the current total number of elements
	// stored by this shard
	GetStoredElements() uint64
	// GetAvgScores Returns the average score for a slice of items, the
	// returned value is a map where the key is the element ID and the
	// value the average clasification for that element
	GetAvgScores([]uint64) map[uint64]float64
	// Stop Stops all the background tasks that are being performed by the
	// recommender like the garbage collector
	Stop()
	// SetMaxElements Sets the max number of elements that can be stored by
	// the recommender shard
	SetMaxElements(maxClassif uint64)
	// SetMaxScore Sets the max score to have in consideration, note that
	// the score starts at 0
	SetMaxScore(maxScore uint8)
	// IsDirty returns true in case of any record was added since the last
	// time the tree was regenerated
	IsDirty() bool

	// DestroyS3Backup Removes all the data stored by this shard on S3
	DestroyS3Backup() (success bool)
}

type score struct {
	recID  uint64
	scores map[uint64]uint8
	next   *score
	prev   *score
}

// Recommender This struct will manage and provide access to a recomender
// system
type Recommender struct {
	Int

	identifier string
	maxScore   uint8
	s3Path     string
	s3Region   aws.Region

	maxClassif   uint64
	totalClassif uint64

	status string

	records       map[uint64]*score
	cloningBuffer map[uint64]map[uint64]uint8
	older         *score
	newer         *score
	// Indicates if any new record was inserted since the last time the
	// tree was recalculated
	dirty   bool
	running bool

	recTree       rectree.BoostrapRecTree
	avgScoreElems map[uint64]float64

	mutex   sync.Mutex
	cloning bool
}

// NewShard Initialize a Recommender objects and returns it, this method also
// launches the background garbage collector based on LRU that is going to
// expire the oldest items
func NewShard(s3Path string, identifier string, maxClassif uint64, maxScore uint8, s3Region string) (rc *Recommender) {
	log.Info("Starting shard:", identifier, "With max number of elements:", maxClassif)

	rc = &Recommender{
		identifier:    identifier,
		maxClassif:    maxClassif,
		totalClassif:  0,
		maxScore:      maxScore,
		records:       make(map[uint64]*score),
		status:        StatusStarting,
		s3Path:        s3Path,
		s3Region:      aws.Regions[s3Region],
		dirty:         true,
		running:       true,
		cloning:       false,
		cloningBuffer: make(map[uint64]map[uint64]uint8),
	}

	go rc.checkAndExpire()

	return
}

// Stop Stops all the background tasks that are being performed by the
// recommender like the garbage collector
func (rc *Recommender) Stop() {
	rc.running = false
}

// SetMaxElements Sets the max number of elements that can be stored by the
// recommender shard
func (rc *Recommender) SetMaxElements(maxClassif uint64) {
	rc.maxClassif = maxClassif
}

// SetMaxScore Sets the max score to have in consideration, note that the score
// starts at 0
func (rc *Recommender) SetMaxScore(maxScore uint8) {
	rc.maxScore = maxScore
}

// GetTotalElements Returns the max number of elements that can ba allocated on
// this recomender shard
func (rc *Recommender) GetTotalElements() uint64 {
	return rc.maxClassif
}

// GetStoredElements Returns the current total number of elements stored by
// this shard
func (rc *Recommender) GetStoredElements() uint64 {
	return rc.totalClassif
}

// GetStatus Returns the current status of this recommender system, the posible
// statuses can be: LOADING, ACTIVE, STARTING, NO_RECORDS
func (rc *Recommender) GetStatus() string {
	return rc.status
}

// GetAvgScores Returns the average score for a slice of items, the returned
// value is a map where the key is the element ID and the value the average
// clasification for that element
func (rc *Recommender) GetAvgScores(itemIDs []uint64) (scores map[uint64]float64) {
	scores = make(map[uint64]float64)
	for _, item := range itemIDs {
		scores[item] = rc.avgScoreElems[item]
	}

	return
}

// CalcScores Calculates the scores for the given records, and stores in memory
// the classification for further processing
func (rc *Recommender) CalcScores(recID uint64, scores map[uint64]uint8, maxToReturn int) (result []uint64) {
	rc.AddRecord(recID, scores)

	if rc.recTree == nil {
		return
	}
	result = rc.recTree.GetBestRecommendation(scores, maxToReturn)

	return
}

// AddRecord Just adds a new record to the recommender system in order to
// increase the knoledge DB
func (rc *Recommender) AddRecord(recID uint64, scores map[uint64]uint8) {
	var sc *score
	var existingRecord bool

	rc.dirty = true
	// If the system is cloning the data to process the tree, just leave
	// the data on the buffer
	if rc.cloning {
		rc.cloningBuffer[recID] = scores
		return
	}
	rc.mutex.Lock()
	if sc, existingRecord = rc.records[recID]; existingRecord {
		if sc.prev != nil {
			sc.prev.next = sc.next
		} else {
			// This is the older elem
			rc.older = rc.older.next
		}
		if sc.next != nil {
			sc.next.prev = sc.prev
		} else {
			// This is the last elem
			rc.newer = rc.newer.prev
		}

		rc.totalClassif += uint64(len(scores) - len(sc.scores))
		sc.scores = scores
	} else {
		sc = &score{
			recID:  recID,
			scores: scores,
		}
		rc.records[recID] = sc
		rc.totalClassif += uint64(len(scores))
	}

	if rc.newer != nil {
		sc.prev = rc.newer
		rc.newer.next = sc
		rc.newer = sc
	} else {
		rc.newer = sc
		rc.older = sc
	}
	rc.mutex.Unlock()

	log.Debug("Stored elements:", rc.totalClassif, "Max stored elements:", rc.maxClassif)
}

// RecalculateTree Lanches the ETL process to create the tree
func (rc *Recommender) RecalculateTree() {
	// No new record was added, so is not necessary to calculate the tree
	// again
	if !rc.dirty {
		log.Info("Tree not dirty:", rc.identifier)
		return
	}
	log.Info("Recalculating tree for:", rc.identifier)
	if len(rc.records) < cMinRecordsToStart {
		rc.dirty = false
		rc.status = StatusNoRecords
		return
	}

	rc.cloning = true
	rc.mutex.Lock()
	records := make([]map[uint64]uint8, len(rc.records))
	i := 0
	for _, record := range rc.records {
		records[i] = record.scores
		i++
	}
	rc.mutex.Unlock()
	rc.cloning = false

	for recID, scores := range rc.cloningBuffer {
		rc.AddRecord(recID, scores)
	}
	rc.cloningBuffer = make(map[uint64]map[uint64]uint8)

	rc.recTree, rc.avgScoreElems = rectree.ProcessNewTrees(records, cRecTreeMaxDeep, rc.maxScore, cRecTreeNumOfTrees)

	rc.status = StatusActive
	log.Info("Tree recalculation finished:", rc.identifier)
	rc.dirty = false
}

// IsDirty returns true in case of any record was added since the last time the
// tree was regenerated
func (rc *Recommender) IsDirty() bool {
	return rc.dirty
}

// DestroyS3Backup Removes all the data stored by this shard on S3
func (rc *Recommender) DestroyS3Backup() (success bool) {
	log.Info("Destroying backup on S3:", rc.identifier)
	auth, err := aws.EnvAuth()
	if err != nil {
		log.Error("Problem trying to connect with AWS:", err)
		return false
	}

	s := s3.New(auth, rc.s3Region)
	bucket := s.Bucket(S3BUCKET)

	if err := bucket.Del(rc.getS3Path()); err != nil {
		log.Info("Problem trying to remove backup from S3:", err)
		return false
	}

	return true
}

// LoadBackup Restores all the information from backup
func (rc *Recommender) LoadBackup() (success bool) {
	log.Info("Loading backup from S3:", rc.identifier)
	auth, err := aws.EnvAuth()
	if err != nil {
		log.Error("Problem trying to connect with AWS:", err)
		return false
	}

	s := s3.New(auth, rc.s3Region)
	bucket := s.Bucket(S3BUCKET)

	jsonData, err := bucket.Get(rc.getS3Path())
	if err != nil {
		log.Info("Problem trying to get backup from S3:", err)
		return false
	}

	dataFromJSON := [][]uint64{}
	json.Unmarshal(rc.uncompress(jsonData), &dataFromJSON)

	log.Info("Data loaded from S3:", rc.identifier, "len:", len(dataFromJSON))
	recs := 0
	for _, record := range dataFromJSON {
		scores := make(map[uint64]uint8)
		for i := 1; i < len(record); i += 2 {
			scores[record[i]] = uint8(record[i+1])
		}
		recs += len(scores)
		rc.AddRecord(record[0], scores)
	}

	return true
}

// SaveBackup Stores all the records serialized in a inexpensive storage system
func (rc *Recommender) SaveBackup() {
	log.Info("Storing backup on S3:", rc.identifier)
	rc.mutex.Lock()
	records := make([][]uint64, len(rc.records))
	i := 0
	for recID, record := range rc.records {
		records[i] = make([]uint64, len(record.scores)*2+1)
		records[i][0] = recID
		elemPos := 1
		for k, v := range record.scores {
			records[i][elemPos] = k
			records[i][elemPos+1] = uint64(v)
			elemPos += 2
		}
		i++
	}
	rc.mutex.Unlock()

	jsonToUpload, err := json.Marshal(records)

	auth, err := aws.EnvAuth()
	if err != nil {
		log.Error("Problem trying to connect with AWS:", err)
		return
	}

	s := s3.New(auth, rc.s3Region)
	bucket := s.Bucket(S3BUCKET)

	err = bucket.Put(
		rc.getS3Path(),
		rc.compress(jsonToUpload),
		"text/plain",
		s3.BucketOwnerFull,
		s3.Options{})
	if err != nil {
		log.Error("Problem trying to upload backup to S3 from:", rc.identifier, "Error:", err)
	}

	log.Info("New backup stored on S3, bucket:", S3BUCKET, "Path:", rc.getS3Path())
}

func (rc *Recommender) getS3Path() string {
	return fmt.Sprintf("%s/%s.json.gz", rc.s3Path, rc.identifier)
}

func (rc *Recommender) uncompress(data []byte) (result []byte) {
	gz, err := gzip.NewReader(strings.NewReader(string(data)))
	if err != nil {
		log.Error("The data can't be uncompressed on shard:", rc.identifier, "Error:", err)
	}
	defer gz.Close()
	if result, err = ioutil.ReadAll(gz); err != nil {
		log.Error("The data can't be uncompressed on shard:", rc.identifier, "Error:", err)
	}

	return
}

func (rc *Recommender) compress(data []byte) (result []byte) {
	var b bytes.Buffer
	gz := gzip.NewWriter(&b)
	if _, err := gz.Write(data); err != nil {
		log.Error("The data can't be compressed on shard:", rc.identifier, "Error:", err)
	}
	if err := gz.Flush(); err != nil {
		log.Error("The data can't be compressed on shard:", rc.identifier, "Error:", err)
	}
	if err := gz.Close(); err != nil {
		log.Error("The data can't be compressed on shard:", rc.identifier, "Error:", err)
	}

	return b.Bytes()
}

func (rc *Recommender) checkAndExpire() {
	for rc.running {
		for rc.totalClassif > rc.maxClassif {
			rc.mutex.Lock()

			rc.totalClassif -= uint64(len(rc.older.scores))
			delete(rc.records, rc.older.recID)
			rc.older = rc.older.next
			rc.older.prev = nil

			rc.mutex.Unlock()
		}

		time.Sleep(time.Millisecond * 300)
	}
}

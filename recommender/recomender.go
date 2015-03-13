package recommender

import (
	"github.com/alonsovidales/go_ml"
	"github.com/alonsovidales/pit/log"
	"runtime"
	"sync"
	"time"
)

const (
	MIN_TO_START_PROCESSING = 5000
)

type RecommenderInt interface {
	CalcScores(recID uint64, scores map[uint64]uint64) (result map[uint64]uint64)
	AddRecord(recID uint64, scores map[uint64]uint64)
}

type score struct {
	recID  uint64
	scores map[uint64]float64
	next   *score
}

type Recommender struct {
	RecommenderInt

	maxMemBytes uint64

	records map[uint64]*score
	older   *score
	newer   *score

	mutex sync.Mutex
}

func Init(maxMemoryMb uint64) (rc *Recommender) {
	log.Info("Configuring max memory ussage to (MB):", maxMemoryMb)

	rc = &Recommender{
		maxMemBytes: uint64(maxMemoryMb) * 1000000,
		records:     make(map[uint64]*score),
	}

	go rc.checkAndExpire()
	go rc.startFiltering()

	return
}

func (rc *Recommender) CalcScores(recID uint64, scores map[uint64]float64, maxToReturn int) (result map[uint64]uint64) {
	//log.Debug("Calculating:", recID, "Scores:", scores)
	rc.AddRecord(recID, scores)

	return
}

func (rc *Recommender) AddRecord(recID uint64, scores map[uint64]float64) {
	//log.Debug("Adding:", recID, "Scores:", scores)
	if _, ok := rc.records[recID]; !ok {
		sc := &score{
			recID:  recID,
			scores: scores,
		}

		rc.mutex.Lock()
		rc.records[recID] = sc
		rc.mutex.Unlock()
		if rc.newer != nil {
			rc.newer.next = sc
			rc.newer = sc
		} else {
			rc.newer = sc
			rc.older = sc
		}
	} else {
		// TODO Touch the element
	}
}

func (rc *Recommender) checkAndExpire() {
	memStats := new(runtime.MemStats)
	for {
		runtime.ReadMemStats(memStats)
		log.Debug("Mem:", memStats.Alloc)
		if rc.maxMemBytes < memStats.Alloc {
			lowBoundary := rc.maxMemBytes - uint64(float64(rc.maxMemBytes)*0.1)
			rc.mutex.Lock()
			for i := 0; lowBoundary < memStats.Alloc && rc.older != nil; i++ {
				delete(rc.records, rc.older.recID)
				rc.older = rc.older.next
				runtime.ReadMemStats(memStats)
				if i%10 == 0 {
					runtime.GC()
				}
			}
			rc.mutex.Unlock()
		}

		time.Sleep(time.Millisecond * 500)
	}
}

func (rc *Recommender) startFiltering() {
	// Wait until have a minimun number of records to start processing
	for len(rc.records) < MIN_TO_START_PROCESSING {
		time.Sleep(time.Millisecond * 500)
	}

	log.Info("Starting to process, records:", len(rc.records))
	for {
		rc.mutex.Lock()
		log.Debug("Building filter:", len(rc.records))
		elemsSet := make(map[uint64]int)
		keys := []uint64{}
		i := 0
		for _, record := range rc.records {
			for k, _ := range record.scores {
				if _, ok := elemsSet[k]; !ok {
					elemsSet[k] = i
					keys = append(keys, k)
					i++
				}
			}
		}

		cf := &ml.CollaborativeFilter{
			Ratings:          make([][]float64, len(keys)),
			AvailableRatings: make([][]float64, len(keys)),
		}

		for i = 0; i < len(keys); i++ {
			cf.Ratings[i] = make([]float64, len(rc.records))
			cf.AvailableRatings[i] = make([]float64, len(rc.records))
		}

		for i, k := range keys {
			rp := 0
			for _, record := range rc.records {
				if v, ok := record.scores[k]; ok {
					cf.Ratings[i][rp] = v
					cf.AvailableRatings[i][rp] = 1.0
				} else {
					cf.AvailableRatings[i][rp] = 0.0
				}
				rp++
			}
		}
		log.Debug("Filter done, training, Keys:", len(keys), "Records:", len(rc.records), "...")
		rc.mutex.Unlock()

		log.Debug("1 Filter done, training...")
		cf.InitializeThetas(len(keys))
		log.Debug("2 Filter done, training...")
		cf.Ratings = cf.Normalize()
		log.Debug("3 Filter done, training...")
		cf.CalcMeans()
		log.Debug("4 Filter done, training...")
		j, _, _ := cf.CostFunction(0.0, false)
		log.Debug("Cost:", j)
		ml.Fmincg(cf, 0, 100, true)
		log.Debug("Training finished...")
	}
}

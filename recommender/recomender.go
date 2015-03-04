package recommender

import (
	//"github.com/alonsovidales/go_ml"
	"github.com/alonsovidales/pit/log"
)

type RecommenderInt interface {
	CalcScores(recID uint64, scores map[uint64]uint64) (result map[uint64]uint64)
	AddRecord(recID uint64, scores map[uint64]uint64)
}

type Recommender struct {
	RecommenderInt

	maxMemBytes uint64
}

func Init(maxMemoryMb int64) (rc *Recommender) {
	log.Info("Configuring max memory ussage to (MB):", maxMemoryMb)

	rc = &Recommender{
		maxMemBytes: uint64(maxMemoryMb) * 1000000,
	}

	return
}

func (rc *Recommender) CalcScores(recID uint64, scores map[uint64]uint64) (result map[uint64]uint64) {
	log.Debug("Calculating:", recID, "Scores:", scores)

	return
}

func (rc *Recommender) AddRecord(recID uint64, scores map[uint64]uint64) {
	log.Debug("Adding:", recID, "Scores:", scores)
}

package rectree

import (
	"github.com/alonsovidales/pit/log"
	"math"
)

const (
	MAX_SCORE = 5
	MIN_SCORE = 0
)

type BoostrapRecTree interface {
	GetBestRecommendation(recordId uint64, recomendations uint64) ([]uint64)
}

type tNode struct {
	value uint64

	like    *tNode
	unknown *tNode
	dislike *tNode
}

type Tree struct {
	BoostrapRecTree

	tree *tNode
	maxDeep int
}

func GetNewTree(records []map[uint64]uint8, maxDeep int) (tr *Tree) {
	var maxFreq, maxElem uint64

	elementsFreq := make(map[uint64]uint64)

	// Get the most commont element in order to be used as root of the tree
	for _, record := range records {
		for k, _ := range record {
			if _, ok := elementsFreq[k]; ok {
				elementsFreq[k]++
			} else {
				elementsFreq[k] = 1
			}

			if maxFreq < elementsFreq[k] {
				maxFreq = elementsFreq[k]
				maxElem = k
			}
		}
	}

	delete(elementsFreq, maxElem)
	tr = &Tree {
		maxDeep: maxDeep,
	}
	tr.tree = tr.getTreeNode(maxElem, elementsFreq, records, maxDeep)

	return
}

func (tr *Tree) GetBestRecommendation(recordId uint64, recomendations uint64) (rec []uint64) {
	return
}

func (tr *Tree) getTreeNode(fromElem uint64, elementsFreq map[uint64]uint64, records []map[uint64]uint8, maxDeep int) (tn *tNode) {
	tn = &tNode {
		value: fromElem,
	}

	if len(records) == 0 {
		return
	}

	// Get the tree best matches for like, dislike or unknown
	// We need likeMinLoss, dislikeMinLoss, unknownMinLoss
	// Split the records on groups of like, dislike, unknown
	groups := [3][]map[uint64]uint8 {
		[]map[uint64]uint8{}, // like
		[]map[uint64]uint8{}, // dislike
		[]map[uint64]uint8{}, // unknown
	}
	for _, record := range records {
		if v, ok := record[fromElem]; ok {
			if v > (MAX_SCORE / 2) {
				groups[0] = append(groups[0], record)
			} else {
				groups[1] = append(groups[1], record)
			}
		} else {
			groups[2] = append(groups[2], record)
		}
	}

	log.Debug("Like:", len(groups[0]), "Dislike:", len(groups[1]),"Unknown:", len(groups[2]))

	likeMinLoss, found := tr.getMinLossElement(groups[0], elementsFreq)
	elementsFreqCopy := tr.copyElemsFreq(elementsFreq)
	if found {
		delete(elementsFreqCopy, likeMinLoss)
		tn.like = tr.getTreeNode(likeMinLoss, elementsFreqCopy, records, maxDeep-1)
		elementsFreqCopy[likeMinLoss] = 1
	}

	dislikeMinLoss, found := tr.getMinLossElement(groups[1], elementsFreq)
	if found {
		delete(elementsFreqCopy, dislikeMinLoss)
		tn.dislike = tr.getTreeNode(dislikeMinLoss, elementsFreqCopy, records, maxDeep-1)
		elementsFreqCopy[dislikeMinLoss] = 1
	}

	//unknownMinLoss := tr.getMinLossElement(groups[2], elementsFreq)
	/*elementsFreqCopy[dislikeMinLoss] = 1
	delete(elementsFreqCopy, unknownMinLoss)
	tn.unknown = tr.getTreeNode(unknownMinLoss, elementsFreqCopy, records, maxDeep-1)*/

	return
}

func (tr *Tree) copyElemsFreq(elems map[uint64]uint64) (cp map[uint64]uint64) {
	cp = make(map[uint64]uint64)

	for k, v := range elems {
		cp[k] = v
	}

	return
}

// getMinLossElement Returns the recordID with the min value on the loss
// function across all the elements, that is the sum of the squares minus the
// square of the sum divided by the number of elements
func (tr *Tree) getMinLossElement(records []map[uint64]uint8, elements map[uint64]uint64) (minLoss uint64, found bool) {
	minLoss = 0
	found = false
	minError := float64(math.Inf(1))
	log.Debug("Estimating for:", len(records), "records...")
	for elem, _ := range elements {
		sumSquares := uint64(0)
		sum := uint64(0)
		elems := uint64(0)
		for _, record := range records {
			if v, ok := record[elem]; ok {
				el64 := uint64(MAX_SCORE - v)
				elems++
				sum += el64
				sumSquares += el64 * el64
			}
		}

		if elems > 0 {
			error := math.Sqrt(float64((sumSquares - (sum * sum)) / elems))
			// The higest score is the one with the min loss
			if minError > error {
				log.Debug("Error:", elem, elems, error)
				minError = error
				minLoss = elem
				found = true
			}
		}
	}

	log.Debug("getMinLossElement:", len(records), len(elements), minLoss)
	return
}

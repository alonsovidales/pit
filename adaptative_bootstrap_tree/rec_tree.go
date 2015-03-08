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

type elemTotals struct {
	elemId uint64
	sum uint64
	sum2 uint64
	n uint64
}

func GetNewTree(records []map[uint64]uint8, maxDeep int) (tr *Tree) {
	var maxFreq, maxElem uint64

	elementsTotals := []elemTotals{}
	elemsPos := make(map[uint64]int)

	// Get the most commont element in order to be used as root of the tree
	// Calculates the sum and the square sum of all the elements on the
	// records
	i := 0
	for _, record := range records {
		for k, v := range record {
			vUint64 := uint64(v)
			v2Uint64 := vUint64*vUint64
			if p, ok := elemsPos[k]; ok {
				elementsTotals[p].sum += vUint64
				elementsTotals[p].sum2 += v2Uint64
				elementsTotals[p].n++
			} else {
				elementsTotals = append(elementsTotals, elemTotals{
					elemId: k,
					sum: vUint64,
					sum2: v2Uint64,
					n: 1,
				})
				elemsPos[k] = i
				i++
			}

			if maxFreq < elementsTotals[elemsPos[k]].n {
				maxFreq = elementsTotals[elemsPos[k]].n
				maxElem = k
			}
		}
	}

	delete(elemsPos, maxElem)
	tr = &Tree {
		maxDeep: maxDeep,
	}
	tr.tree = tr.getTreeNode(maxElem, elemsPos, elementsTotals, records)

	return
}

func (tr *Tree) GetBestRecommendation(recordId uint64, recomendations uint64) (rec []uint64) {
	return
}

type elemSums struct {
	sumL uint64
	sum2L uint64
	nL uint64
	sumH uint64
	sum2H uint64
	nH uint64
	sumU uint64
	sum2U uint64
	nU uint64
	errL float64
	errH float64
	errU float64
}

func (tr *Tree) getTreeNode(fromElem uint64, elemsPos map[uint64]int, elementsTotals []elemTotals, records []map[uint64]uint8) (tn *tNode) {
	tn = &tNode {
		value: fromElem,
	}

	if len(elemsPos) == 0 {
		return
	}

	totals := make([]elemSums, len(elementsTotals))

	for _, record := range records {
		if v, ok := record[fromElem]; ok {
			// We have a score for this element on this record
			// calculate the totals
			for elem, pos := range elemsPos {
				if j, isJ := record[elem]; isJ {
					jUint64 := uint64(j)
					j2Uint64 := jUint64*jUint64

					if v >= (MAX_SCORE / 2) {
						totals[pos].sumL += jUint64
						totals[pos].sum2L += j2Uint64
						totals[pos].nL++
					} else {
						totals[pos].sumH += jUint64
						totals[pos].sum2H += j2Uint64
						totals[pos].nH++
					}
				}
			}
		}
	}

	for _, pos := range elemsPos {
		totals[pos].sumU = elementsTotals[pos].sum - totals[pos].sumL - totals[pos].sumH
		totals[pos].sum2U = elementsTotals[pos].sum2 - totals[pos].sum2L - totals[pos].sum2H
		totals[pos].nU = elementsTotals[pos].n - totals[pos].nL - totals[pos].nH
	}

	// Compute the error for each element
	var maxLike, maxHate, maxUnknown uint64
	maxScoreL := 0.0
	maxScoreH := 0.0
	maxScoreU := 0.0
	for _, pos := range elemsPos {
		scoreL := float64((totals[pos].sumL*totals[pos].sumL) - totals[pos].sum2L) / float64(totals[pos].nL)
		scoreH := float64((totals[pos].sumH*totals[pos].sumH) - totals[pos].sum2H) / float64(totals[pos].nH)
		scoreU := float64((totals[pos].sumU*totals[pos].sumU) - totals[pos].sum2U) / float64(totals[pos].nU)

		if maxScoreL < scoreL {
			maxScoreL = scoreL
			maxLike = elementsTotals[pos].elemId
		}
		if maxScoreH < scoreH {
			maxScoreH = scoreH
			maxHate = elementsTotals[pos].elemId
		}
		if maxScoreU < scoreU {
			maxScoreU = scoreU
			maxUnknown = elementsTotals[pos].elemId
		}
	}

	log.Debug("MaxsL:", maxScoreL, maxLike, totals[elemsPos[maxLike]], float64(totals[elemsPos[maxLike]].sumL) / float64(totals[elemsPos[maxLike]].nL), totals[elemsPos[maxLike]].nL)
	log.Debug("MaxsH:", maxScoreH, maxHate, totals[elemsPos[maxHate]], float64(totals[elemsPos[maxHate]].sumH) / float64(totals[elemsPos[maxHate]].nH), totals[elemsPos[maxLike]].nH)
	log.Debug("MaxsU:", maxScoreU, maxUnknown, totals[elemsPos[maxUnknown]], float64(totals[elemsPos[maxUnknown]].sumU) / float64(totals[elemsPos[maxUnknown]].nU), totals[elemsPos[maxLike]].nU)

	return

	//func (tr *Tree) getTreeNode(fromElem uint64, elemsPos map[uint64]int, elementsTotals []elemTotals, records []map[uint64]uint8) (tn *tNode) {
	pos := elemsPos[maxLike]
	delete(elemsPos, maxLike)
	tn.like = tr.getTreeNode(maxLike, elemsPos, elementsTotals, records)
	elemsPos[maxLike] = pos

	pos = elemsPos[maxHate]
	delete(elemsPos, maxHate)
	tn.like = tr.getTreeNode(maxHate, elemsPos, elementsTotals, records)
	elemsPos[maxHate] = pos

	pos = elemsPos[maxUnknown]
	delete(elemsPos, maxUnknown)
	tn.like = tr.getTreeNode(maxUnknown, elemsPos, elementsTotals, records)
	elemsPos[maxUnknown] = pos

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

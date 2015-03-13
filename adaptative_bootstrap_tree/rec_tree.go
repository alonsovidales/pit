package rectree

import (
	"github.com/alonsovidales/pit/log"
	"sort"
)

const (
	maxSecondaryElements = 20
)

// BoostrapRecTree All the structs that implements this interface has to be
// able to process a list of records and return a list of recomended items
// based on the previously classified items
type BoostrapRecTree interface {
	// GetBestRecommendation Using the classification of the elements from
	// the first values, this method process and returns a list of up to
	// maxRecs items IDs
	GetBestRecommendation(values map[uint64]uint8, maxRecs int) (rec []uint64)
}

// Tree Used to process and return lists of recommended items
type Tree struct {
	BoostrapRecTree

	tree      map[uint64]*tNode
	maxDeep   int
	maxScore  uint8
	totalRecs int

	numOfTrees int

	// Flag used to return all the records even the yet classified, used
	// for test proposals only
	testMode bool
}

type tNode struct {
	value uint64

	bestRecL []*scoresClassifications
	bestRecU []*scoresClassifications
	bestRecD []*scoresClassifications

	like    *tNode
	unknown *tNode
	dislike *tNode
}

type elemTotals struct {
	elemID uint64
	sum    uint64
	sum2   uint64
	err    uint64
	n      uint64
}

type elemSums struct {
	sumL   uint64
	sum2L  uint64
	nL     uint64
	sumH   uint64
	sum2H  uint64
	nH     uint64
	sumU   uint64
	sum2U  uint64
	nU     uint64
	scoreL float64
	scoreH float64
	scoreU float64
}

type scoresClassifications struct {
	score  float64
	avg    float64
	elemID uint64
}

type byClassif []*scoresClassifications

func (a byClassif) Len() int           { return len(a) }
func (a byClassif) Swap(i, j int)      { a[i], a[j] = a[j], a[i] }
func (a byClassif) Less(i, j int) bool { return a[i].score > a[j].score }

// ProcessNewTrees Creates a new set of numberOfTrees decission trees with a
// max deep of maxDeep. Specify on maxScore the max possible score for the
// elements
func ProcessNewTrees(records []map[uint64]uint8, maxDeep int, maxScore uint8, numberOfTrees int) (tr *Tree) {
	elementsTotals := []elemTotals{}
	elemsPos := make(map[uint64]int)

	// Get the most commont element in order to be used as root of the tree
	// Calculates the sum and the square sum of all the elements on the
	// records
	i := 0
	for _, record := range records {
		for k, v := range record {
			vUint64 := uint64(v)
			v2Uint64 := vUint64 * vUint64
			if p, ok := elemsPos[k]; ok {
				elementsTotals[p].sum += vUint64
				elementsTotals[p].sum2 += v2Uint64
				elementsTotals[p].err += uint64((maxScore - v) * (maxScore - v))
				elementsTotals[p].n++
			} else {
				elementsTotals = append(elementsTotals, elemTotals{
					elemID: k,
					sum:    vUint64,
					sum2:   v2Uint64,
					err:    uint64((maxScore - v) * (maxScore - v)),
					n:      1,
				})
				elemsPos[k] = i
				i++
			}
		}
	}

	tr = &Tree{
		maxDeep:   maxDeep,
		maxScore:  maxScore,
		totalRecs: len(records),
		tree:      make(map[uint64]*tNode),
		testMode:  false,
	}

	if len(elemsPos) < numberOfTrees {
		tr.numOfTrees = len(elemsPos)
	} else {
		tr.numOfTrees = numberOfTrees
	}

	i = 0
	for element, pos := range elemsPos {
		if i >= tr.numOfTrees {
			return
		}

		log.Debug("----->>>> Building tree from:", element, i)
		delete(elemsPos, element)
		tr.tree[element] = tr.getTreeNode(element, elemsPos, elementsTotals, records, 1)
		elemsPos[element] = pos

		i++
	}

	return
}

// GetBestRecommendation Using the classification of the elements from the
// first values, this method process and returns a list of up to maxRecs items
// IDs
func (tr *Tree) GetBestRecommendation(values map[uint64]uint8, maxRecs int) (rec []uint64) {
	// Will store all the recomendations by level, as deeper as best
	bestRecsByLevels := [][]uint64{}
	secondaryByLevels := [][]*scoresClassifications{}

	for elemID, tree := range tr.tree {
		level := 0
		for tree != nil {
			if len(bestRecsByLevels) <= level {
				bestRecsByLevels = append(bestRecsByLevels, []uint64{tree.value})
				secondaryByLevels = append(secondaryByLevels, []*scoresClassifications{})
			} else {
				bestRecsByLevels[level] = append(bestRecsByLevels[level], tree.value)
			}

			if score, classified := values[elemID]; classified {
				if score >= tr.maxScore/2 {
					if tree.bestRecL != nil {
						secondaryByLevels[level] = append(secondaryByLevels[level], tree.bestRecL...)
					}
					tree = tree.like
				} else {
					if tree.bestRecD != nil {
						secondaryByLevels[level] = append(secondaryByLevels[level], tree.bestRecD...)
					}
					tree = tree.dislike
				}
			} else {
				if tree.bestRecU != nil {
					secondaryByLevels[level] = append(secondaryByLevels[level], tree.bestRecU...)
				}
				tree = tree.unknown
			}
			level++
		}
	}

	//log.Debug("Scores by levels:", bestRecsByLevels)
	//log.Debug("Levels:", len(bestRecsByLevels))

	recMap := make(map[uint64]bool)
	for i := len(bestRecsByLevels) - 1; i >= 0 && len(rec) < maxRecs; i-- {
		for _, elem := range bestRecsByLevels[i] {
			if _, classified := values[elem]; !classified || tr.testMode {
				recMap[elem] = true
			}
		}
	}

	// Populate the list of recomended elements
secondaryElemsLoop:
	for i := len(bestRecsByLevels) - 1; i >= 0 && len(rec) < maxRecs; i-- {
		sort.Sort(byClassif(secondaryByLevels[i]))

		for _, elem := range secondaryByLevels[i] {
			if _, classified := values[elem.elemID]; !classified || tr.testMode {
				recMap[elem.elemID] = true
				if len(rec) >= maxRecs {
					break secondaryElemsLoop
				}
			}
		}
	}

	i := 0
	rec = make([]uint64, len(recMap))
	for k := range recMap {
		rec[i] = k
		i++
	}

	return
}

func (tr *Tree) setTestMode() {
	tr.testMode = true
}

func (tr *Tree) getTreeNode(fromElem uint64, elemsPos map[uint64]int, elementsTotals []elemTotals, records []map[uint64]uint8, deep int) (tn *tNode) {
	tn = &tNode{
		value: fromElem,
	}

	if len(elemsPos) == 0 {
		return
	}
	// In case of this is the last node because of the max deep or that the
	// set is not enought big, we will store all the list of pending movies
	// on this node
	lastNode := deep > tr.maxDeep || len(records) < tr.totalRecs/10

	totals := make([]elemSums, len(elementsTotals))

	likeRecords := []map[uint64]uint8{}
	hateRecords := []map[uint64]uint8{}
	unknownRecords := []map[uint64]uint8{}

	for _, record := range records {
		if v, ok := record[fromElem]; ok {
			if v >= tr.maxScore/2 {
				likeRecords = append(likeRecords, record)
			} else {
				hateRecords = append(hateRecords, record)
			}

			// We have a score for this element on this record
			// calculate the totals
			for elem, pos := range elemsPos {
				if j, isJ := record[elem]; isJ {
					jUint64 := uint64(j)
					j2Uint64 := jUint64 * jUint64

					if v >= tr.maxScore/2 {
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
		} else {
			unknownRecords = append(unknownRecords, record)
		}
	}

	for _, pos := range elemsPos {
		totals[pos].sumU = elementsTotals[pos].sum - totals[pos].sumL - totals[pos].sumH
		totals[pos].sum2U = elementsTotals[pos].sum2 - totals[pos].sum2L - totals[pos].sum2H
		totals[pos].nU = elementsTotals[pos].n - totals[pos].nL - totals[pos].nH
	}

	// Compute the error for each element
	var maxLike, maxHate, maxUnknown uint64
	var pos int
	maxScoreL := 0.0
	maxScoreH := 0.0
	maxScoreU := 0.0

	classifsL := []*scoresClassifications{}
	classifsD := []*scoresClassifications{}
	classifsU := []*scoresClassifications{}
	var scoreL, scoreH, scoreU float64
	for _, pos := range elemsPos {
		if totals[pos].nL > 0 {
			scoreL = float64(totals[pos].sumL*totals[pos].sumL-totals[pos].sum2L) / float64(totals[pos].nL)
			if lastNode && uint8(totals[pos].sumL/totals[pos].nL) > tr.maxScore/2 {
				classifsL = append(classifsL, &scoresClassifications{
					score:  scoreL,
					elemID: elementsTotals[pos].elemID,
					avg:    float64(totals[pos].sumL) / float64(totals[pos].nL),
				})
			}
		} else {
			scoreL = 0
		}
		if totals[pos].nH > 0 {
			scoreH = float64(totals[pos].sumH*totals[pos].sumH-totals[pos].sum2H) / float64(totals[pos].nH)
			if lastNode && uint8(totals[pos].sumH/totals[pos].nH) > tr.maxScore/2 {
				classifsD = append(classifsD, &scoresClassifications{
					score:  scoreH,
					elemID: elementsTotals[pos].elemID,
					avg:    float64(totals[pos].sumH) / float64(totals[pos].nH),
				})
			}
		} else {
			scoreH = 0
		}
		if totals[pos].nU > 0 {
			scoreU = float64(totals[pos].sumU*totals[pos].sumU-totals[pos].sum2U) / float64(totals[pos].nU)
			if lastNode && uint8(totals[pos].sumU/totals[pos].nU) > tr.maxScore/2 {
				classifsU = append(classifsU, &scoresClassifications{
					score:  scoreU,
					elemID: elementsTotals[pos].elemID,
					avg:    float64(totals[pos].sumU) / float64(totals[pos].nU),
				})
			}
		} else {
			scoreU = 0
		}

		if maxScoreL < scoreL {
			maxScoreL = scoreL
			maxLike = elementsTotals[pos].elemID
		}
		if maxScoreH < scoreH {
			maxScoreH = scoreH
			maxHate = elementsTotals[pos].elemID
		}
		if maxScoreU < scoreU {
			maxScoreU = scoreU
			maxUnknown = elementsTotals[pos].elemID
		}
	}

	if lastNode {
		sort.Sort(byClassif(classifsL))
		sort.Sort(byClassif(classifsD))
		sort.Sort(byClassif(classifsU))

		if len(classifsL) > maxSecondaryElements {
			tn.bestRecL = classifsL[:maxSecondaryElements]
		} else {
			tn.bestRecL = classifsL
		}
		if len(classifsD) > maxSecondaryElements {
			tn.bestRecD = classifsD[:maxSecondaryElements]
		} else {
			tn.bestRecD = classifsD
		}
		if len(classifsU) > maxSecondaryElements {
			tn.bestRecU = classifsU[:maxSecondaryElements]
		} else {
			tn.bestRecU = classifsU
		}
	} else {
		/*log.Debug("MaxsL:", maxScoreL, maxLike, "Avg:", float64(totals[elemsPos[maxLike]].sumL)/float64(totals[elemsPos[maxLike]].nL), "Deep:", deep, totals[elemsPos[maxLike]])
		log.Debug("MaxsH:", maxScoreH, maxHate, "Avg:", float64(totals[elemsPos[maxHate]].sumH)/float64(totals[elemsPos[maxHate]].nH), "Deep:", deep, totals[elemsPos[maxLike]])
		log.Debug("MaxsU:", maxScoreU, maxUnknown, "Avg:", float64(totals[elemsPos[maxUnknown]].sumU)/float64(totals[elemsPos[maxUnknown]].nU), "Deep:", deep, totals[elemsPos[maxLike]])*/

		if totals[elemsPos[maxLike]].nL > 0 && totals[elemsPos[maxLike]].sumL/totals[elemsPos[maxLike]].nL >= uint64(tr.maxScore/2+1) {
			pos = elemsPos[maxLike]
			delete(elemsPos, maxLike)
			tn.like = tr.getTreeNode(maxLike, elemsPos, elementsTotals, likeRecords, deep+1)
			elemsPos[maxLike] = pos
		}

		if totals[elemsPos[maxLike]].nH > 0 && totals[elemsPos[maxHate]].sumH/totals[elemsPos[maxLike]].nH >= uint64(tr.maxScore/2+1) {
			pos = elemsPos[maxHate]
			delete(elemsPos, maxHate)
			tn.dislike = tr.getTreeNode(maxHate, elemsPos, elementsTotals, hateRecords, deep+1)
			elemsPos[maxHate] = pos
		}

		if totals[elemsPos[maxLike]].nU > 0 && totals[elemsPos[maxUnknown]].sumU/totals[elemsPos[maxLike]].nU >= uint64(tr.maxScore/2+1) {
			pos = elemsPos[maxUnknown]
			delete(elemsPos, maxUnknown)
			tn.unknown = tr.getTreeNode(maxUnknown, elemsPos, elementsTotals, unknownRecords, deep+1)
			elemsPos[maxUnknown] = pos
		}
	}

	return
}

func (tr *Tree) printTree() {
	queue := []*tNode{tr.tree[0]}

	for len(queue) > 0 {
		node := queue[0]
		queue = queue[1:]

		recsL := make([][2]float64, len(node.bestRecL))
		for i := 0; i < len(recsL); i++ {
			recsL[i][0] = node.bestRecL[i].score
			recsL[i][1] = node.bestRecL[i].avg
		}
		recsD := make([][2]float64, len(node.bestRecD))
		for i := 0; i < len(recsD); i++ {
			recsD[i][0] = node.bestRecD[i].score
			recsD[i][1] = node.bestRecD[i].avg
		}
		recsU := make([][2]float64, len(node.bestRecU))
		for i := 0; i < len(recsU); i++ {
			recsU[i][0] = node.bestRecU[i].score
			recsU[i][1] = node.bestRecU[i].avg
		}
		log.Debug("From:", node.value)
		log.Debug("Like:", recsL)
		log.Debug("Disl:", recsD)
		log.Debug("Unkn:", recsU)

		if node.like != nil {
			queue = append(queue, node.like)
		}
		if node.unknown != nil {
			queue = append(queue, node.unknown)
		}
		if node.dislike != nil {
			queue = append(queue, node.dislike)
		}
	}
}

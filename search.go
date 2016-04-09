// Copyright 2016 Google Inc. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//    http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package codesearch

import (
	"fmt"
	"log"
	"sort"
)

var _ = log.Println

// All the matches for a given file.
type mergedCandidateMatch struct {
	fileID  uint32
	matches map[*SubstringQuery][]candidateMatch
}

func mergeCandidates(iters []*docIterator) []mergedCandidateMatch {
	var cands [][]candidateMatch
	for _, i := range iters {
		iterCands := i.next()
		cands = append(cands, iterCands)
	}

	var merged []mergedCandidateMatch
	var nextDoc uint32

done:
	for {
		found := true
		var newCands [][]candidateMatch
		for _, ms := range cands {
			for len(ms) > 0 && ms[0].file < nextDoc {
				ms = ms[1:]
			}
			if len(ms) == 0 {
				break done
			}
			if ms[0].file > nextDoc {
				nextDoc = ms[0].file
				found = false
			}
			newCands = append(newCands, ms)
		}
		cands = newCands
		if !found {
			continue
		}

		newCands = newCands[:0]
		mc := mergedCandidateMatch{
			fileID: nextDoc,
			matches: map[*SubstringQuery][]candidateMatch{},
		}
		for _, ms := range cands {
			var sqMatches []candidateMatch
			for len(ms) > 0 && ms[0].file == nextDoc {
				sqMatches = append(sqMatches, ms[0])
				ms = ms[1:]
			}

			mc.matches[sqMatches[0].query] = sqMatches
			newCands = append(newCands, ms[:])
		}

		merged = append(merged, mc)
	}

	return merged
}

func (s *searcher) andSearch(andQ *andQuery) ([]FileMatch, error) {
	var caseSensitive bool
	var iters []*docIterator
	for _, atom := range andQ.atoms {
		if atom.Negate {
			return nil, fmt.Errorf("not implemented: negation")
		}
		caseSensitive = caseSensitive || atom.CaseSensitive

		// TODO - postingsCache
		i, err := s.reader.getDocIterator(s.indexData, atom)
		if err != nil {
			return nil, err
		}
		iters = append(iters, i)
	}

	// TODO merge mergeCandidates and following loop.
	cands := mergeCandidates(iters)

	var fileMatches []FileMatch
	lastFile := uint32(0xFFFFFFFF)
	var content []byte
	var caseBits []byte
	var newlines []uint32

nextFileMatch:
	for _, c := range cands {
		if lastFile != c.fileID {
			// needed for caseSensitive and for
			// reconstructing the data.
			caseBits = s.reader.readCaseBits(s.indexData, c.fileID)
		}

		if caseSensitive {
			trimmed := map[*SubstringQuery][]candidateMatch{}
			for q, req := range c.matches {
				matching := []candidateMatch{}
				for _, m := range req {
					if m.caseMatches(caseBits) {
						matching = append(matching, m)
					}
				}
				if len(matching) == 0 {
					continue nextFileMatch
				}
				trimmed[q] = matching
			}

			c.matches = trimmed
		}

		if lastFile != c.fileID {
			content = s.reader.readContents(s.indexData, c.fileID)
			newlines = s.reader.readNewlines(s.indexData, c.fileID)
			lastFile = c.fileID
		}

		trimmed := map[*SubstringQuery][]candidateMatch{}
		for q, req := range c.matches {
			matching := []candidateMatch{}
			for _, m := range req {
				if m.matchContent(content) {
					matching = append(matching, m)
				}
			}
			if len(matching) == 0 {
				continue nextFileMatch
			}
			trimmed[q] = matching
		}
		c.matches = trimmed

		fMatch := FileMatch{
			Name: s.indexData.fileNames[c.fileID],
			Rank: int(c.fileID),
		}

		for _, req := range c.matches {
			for _, m := range req {
				num, off, data := m.line(newlines, content, caseBits)
				fMatch.Matches = append(fMatch.Matches,
					Match{
						Offset: m.offset,
						Line: string(data),
						LineNum:     num,
						LineOff:     off,
						MatchLength: len(m.substrBytes),
					})
			}
		}

		sortMatches(fMatch.Matches)
		fileMatches = append(fileMatches, fMatch)

	}
	return fileMatches, nil
}


type matchOffsetSlice []Match
func (m matchOffsetSlice) Len() int { return len(m) }
func (m matchOffsetSlice) Swap(i, j int) { m[i],m[j] = m[j],m[i] }
func (m matchOffsetSlice) Less(i, j int) bool { return m[i].Offset <= m[j].Offset }

func sortMatches(ms []Match) {
	sort.Sort(matchOffsetSlice(ms))
}

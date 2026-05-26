// Copyright 2026 The Bazel Authors. All rights reserved.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of it at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package python

import (
	"context"

	"github.com/emirpasic/gods/sets/treeset"
	"golang.org/x/sync/errgroup"
)

func (p *python3Parser) parseFiles(pyFilenames *treeset.Set) ([]*ParserOutput, error) {
	g, ctx := errgroup.WithContext(context.Background())
	ch := make(chan struct{}, 6) // Limit the number of concurrent parses.
	chRes := make(chan *ParserOutput, len(pyFilenames.Values()))
	for _, v := range pyFilenames.Values() {
		ch <- struct{}{}
		g.Go(func(filename string) func() error {
			return func() error {
				defer func() {
					<-ch
				}()
				res, err := NewFileParser().ParseFile(ctx, p.repoRoot, p.relPackagePath, filename)
				if err != nil {
					return err
				}
				chRes <- res
				return nil
			}
		}(v.(string)))
	}
	if err := g.Wait(); err != nil {
		return nil, err
	}
	close(ch)
	close(chRes)

	results := make([]*ParserOutput, 0, len(pyFilenames.Values()))
	for res := range chRes {
		results = append(results, res)
	}
	return results, nil
}

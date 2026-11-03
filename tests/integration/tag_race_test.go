package integration

import (
	"fmt"
	"net/http"
	"sync"
	"testing"
)

func TestTagCreation_ConcurrentTasksWithSameNewTagAllSucceed(t *testing.T) {
	tc := newTestContext(t)
	registerAndLogin(t, tc)
	spaceID := createSpace(t, tc)
	listID := createList(t, tc, spaceID)

	rec := request(t, tc.Router, http.MethodPost, "/api/v1/lists/"+itoa(listID)+"/statuses", tc.Token,
		map[string]any{"name": "Todo", "color": "#112233", "type": 1, "list_id": listID})
	if rec.Code != http.StatusCreated {
		t.Fatalf("create status: %d: %s", rec.Code, rec.Body.String())
	}
	statusID := responseID(t, rec)

	var (
		mu     sync.Mutex
		failed []string
	)
	for round := 0; round < 8; round++ {
		var wg sync.WaitGroup
		for i := 0; i < 30; i++ {
			wg.Add(1)
			go func(i int) {
				defer wg.Done()
				r := request(t, tc.Router, http.MethodPost, "/api/v1/lists/"+itoa(listID)+"/tasks", tc.Token, map[string]any{
					"title": fmt.Sprintf("race %d-%d", round, i), "status_id": statusID,
					"new_tag_names": []string{fmt.Sprintf("shared-%d", round)},
				})
				if r.Code != http.StatusCreated {
					mu.Lock()
					failed = append(failed, fmt.Sprintf("%d: %s", r.Code, r.Body.String()))
					mu.Unlock()
				}
			}(i)
		}
		wg.Wait()
	}
	if len(failed) > 0 {
		t.Fatalf("%d of 240 concurrent creates failed, e.g. %s", len(failed), failed[0])
	}

	var tags int64
	tc.DB.Table("tags").Where("space_id = ?", spaceID).Count(&tags)
	if tags != 8 {
		t.Fatalf("tags created = %d, want exactly 8 (one per shared name)", tags)
	}
}

package suxinvideo

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"testing"

	"github.com/go-sql-driver/mysql"
)

func TestVodAliasApprovedScopeAndClosure(t *testing.T) {
	report := []byte(`{"entries":[{"id":1,"name":"已授权标题","candidate_details":[{"id":2,"status":"待核实"},{"id":3,"status":"身份匹配"}]}]}`)
	variants := []byte(`[{"hongguo_id":2,"candidate_id":4,"hongguo_title":"已授权标题","candidate_title":"已授权标题第二季","status":"待核实；完整标题不同，未自动合并"},{"hongguo_id":5,"candidate_id":6,"hongguo_title":"排除第一季","candidate_title":"排除第二季","status":"已确认不同季，保留"}]`)
	edges, excluded, err := vodAliasApprovedEdges(report, variants)
	if err != nil || len(edges) != 2 || len(excluded) != 1 {
		t.Fatal("maintenance included an unapproved identity match or excluded season")
	}
	films := []row{{"id": 1, "name": "已授权标题"}, {"id": 2, "name": "已授权标题"}, {"id": 4, "name": "已授权标题第二季"}}
	before, _ := json.Marshal(films)
	groups, err := buildVodAliasGroups(edges, excluded, films, nil)
	if err != nil || len(groups) != 1 || groups[0].CanonicalID != 1 || !reflect.DeepEqual(groups[0].IDs, []int64{1, 2, 4}) {
		t.Fatal("explicit shared-title closure did not preserve distinct original IDs")
	}
	after, _ := json.Marshal(films)
	if string(before) != string(after) {
		t.Fatal("planning changed original films")
	}
	_, err = buildVodAliasGroups(edges, []vodAliasEdge{{A: 1, B: 4}}, films, nil)
	if err == nil {
		t.Fatal("transitive grouping ignored a confirmed excluded pair")
	}
	films[1]["name"] = "管理员后来更改的标题"
	if _, err := buildVodAliasGroups(edges, excluded, films, nil); err == nil {
		t.Fatal("a stale report renamed or grouped a changed film")
	}
}

func TestVodAliasRetryOnlyTransientConflicts(t *testing.T) {
	attempts := []int{}
	err := vodAliasRetryTransaction(context.Background(), func(attempt int) error {
		attempts = append(attempts, attempt)
		if attempt < 3 {
			return fmt.Errorf("private diagnostic: %w", &mysql.MySQLError{Number: 1213, Message: "deadlock fixture"})
		}
		return nil
	})
	if err != nil || !reflect.DeepEqual(attempts, []int{1, 2, 3}) {
		t.Fatal("a transient rolled-back transaction was not retried with distinct attempt IDs")
	}
	attempts = nil
	err = vodAliasRetryTransaction(context.Background(), func(attempt int) error {
		attempts = append(attempts, attempt)
		return errors.New("backup could not be saved")
	})
	if err == nil || len(attempts) != 1 {
		t.Fatal("a backup or validation failure was retried instead of stopping writes")
	}
	attempts = nil
	err = vodAliasRetryTransaction(context.Background(), func(attempt int) error {
		attempts = append(attempts, attempt)
		return &mysql.MySQLError{Number: 1213, Message: "persistent fixture"}
	})
	if err == nil || len(attempts) != 3 {
		t.Fatal("retry bound did not stop persistent lock conflicts")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err = vodAliasRetryTransaction(ctx, func(int) error { return &mysql.MySQLError{Number: 1205, Message: "timeout fixture"} })
	if !errors.Is(err, context.Canceled) {
		t.Fatal("cancelled maintenance ignored its stop signal during retry backoff")
	}
}

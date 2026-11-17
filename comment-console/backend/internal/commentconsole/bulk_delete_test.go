package commentconsole

import "testing"

func TestBulkDeletePreservesSendRecordsAndTheirPosts(t *testing.T) {
	e := testEngine(t, DemoBackend{})
	if err := e.update(func(s *State) error {
		s.Posts = []Post{{ID: "plain"}, {ID: "queued-post"}, {ID: "sent-post"}}
		s.Comments = []Comment{
			{ID: "queued", PostID: "queued-post", Status: "queued"},
			{ID: "sent", PostID: "sent-post", Status: "sent"},
			{ID: "sending", PostID: "sent-post", Status: "sending"},
			{ID: "uncertain", PostID: "sent-post", Status: "uncertain"},
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	deleted, protected, err := e.DeletePosts([]string{"plain", "plain", "queued-post", "sent-post"})
	if err != nil || deleted != 1 || protected != 2 {
		t.Fatalf("候选删除结果：deleted=%d protected=%d err=%v", deleted, protected, err)
	}
	deleted, protected, err = e.DeleteComments([]string{"queued", "sent", "sending", "uncertain"})
	if err != nil || deleted != 1 || protected != 3 {
		t.Fatalf("评论删除结果：deleted=%d protected=%d err=%v", deleted, protected, err)
	}
	deleted, protected, err = e.DeletePosts([]string{"queued-post", "sent-post"})
	state := e.Snapshot()
	if err != nil || deleted != 1 || protected != 1 || len(state.Comments) != 3 || len(state.Posts) != 1 || state.Posts[0].ID != "sent-post" {
		t.Fatalf("最终记录：deleted=%d protected=%d comments=%d posts=%d err=%v", deleted, protected, len(state.Comments), len(state.Posts), err)
	}
}

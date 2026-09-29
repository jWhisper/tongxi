package app

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"tongxi/internal/store"
)

func TestImagesSharedReadsFreshFilesAndScopedHistory(t *testing.T) {
	s, c, members := groupService(t)
	var b bytes.Buffer
	if err := png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 64, 32))); err != nil {
		t.Fatal(err)
	}
	file, err := s.ImportImage(c.ID, "poster.png", base64.StdEncoding.EncodeToString(b.Bytes()))
	if err != nil {
		t.Fatal(err)
	}
	preview, err := s.ImagePreview(c.ID, file.ID, true)
	if err != nil || preview.Width != 64 {
		t.Fatal(err)
	}
	reads, err := s.db.ImageReads(c.ID)
	if err != nil || len(reads) != 0 {
		t.Fatal("preview recorded as model read", err)
	}
	if _, err = s.ImagePreview("other-conversation", file.ID, false); err == nil {
		t.Fatal("cross-conversation image access")
	}
	if _, _, err = s.imageFile(context.Background(), c.ID, "", "../poster.png"); err == nil {
		t.Fatal("path traversal")
	}
	s.newChatModel = func(context.Context, string, string, string) (model.ToolCallingChatModel, error) {
		return &collaborationModel{next: func(_ context.Context, in []*schema.Message, tools map[string]bool) (*schema.Message, error) {
			if !tools["read_image"] {
				t.Error("image tool missing")
			}
			last := in[len(in)-1]
			if last.Role != schema.Tool {
				return toolMessage("read_image", `{"source_id":"`+file.ID+`"}`), nil
			}
			if len(last.UserInputMultiContent) != 2 || last.UserInputMultiContent[1].Image == nil {
				t.Error("tool did not return actual image")
			}
			return schema.AssistantMessage("已查看海报", nil), nil
		}}, nil
	}
	events := make(chan store.ConversationRun, 1000)
	s.StartScheduler(func(r store.ConversationRun) { events <- r })
	for i := 0; i < 2; i++ {
		d, err := s.Schedule(store.ScheduleRequest{RequestID: newID(), ConversationID: c.ID, Content: "查看这张图", Action: "mention", AgentIDs: []string{members[i].ID}, AttachmentIDs: []string{file.ID}})
		if err != nil {
			t.Fatal(err)
		}
		waitRun(t, events, d.Runs[0].ID, "completed")
		history, err := s.db.ContextHistory(members[i].ID, c.ID, "reply")
		if err != nil {
			t.Fatal(err)
		}
		data, _ := json.Marshal(history)
		if strings.Contains(string(data), "base64,") {
			t.Fatal("image persisted in history")
		}
		transcript, err := s.db.ModelHistory(members[i].ID, c.ID)
		if err != nil {
			t.Fatal(err)
		}
		data, _ = json.Marshal(transcript)
		if strings.Contains(string(data), "base64,") {
			t.Fatal("raw transcript persisted pixels")
		}
		if i == 0 {
			b.Reset()
			_ = png.Encode(&b, image.NewRGBA(image.Rect(0, 0, 80, 40)))
			if err = os.WriteFile(filepath.Join(c.WorkDir, file.Name), b.Bytes(), 0600); err != nil {
				t.Fatal(err)
			}
		}
	}
	detail, err := s.Conversation(c.ID)
	if err != nil || len(detail.ImageReads) != 2 || detail.ImageReads[0].Hash == detail.ImageReads[1].Hash || detail.ImageReads[1].Width != 80 {
		t.Fatal("fresh image/individual receipts missing", err)
	}
	if _, err = s.ImportImage(c.ID, "../bad.png", base64.StdEncoding.EncodeToString(b.Bytes())); err == nil {
		t.Fatal("upload path traversal")
	}
	if _, err = s.ImportImage(c.ID, "bad.png", base64.StdEncoding.EncodeToString([]byte("bad"))); err == nil {
		t.Fatal("corrupt upload accepted")
	}
}

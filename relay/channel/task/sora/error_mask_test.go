package sora

import (
	"strings"
	"testing"

	"github.com/QuantumNous/new-api/model"
)

func TestConvertToOpenAIVideo_MasksErrorMessage(t *testing.T) {
	a := &TaskAdaptor{}
	task := &model.Task{
		TaskID: "task_pub1",
		Data:   []byte(`{"id":"video_up1","status":"failed","error":{"message":"fetch https://internal.vendor.com/v1/asset failed","code":"asset_error"}}`),
	}
	out, err := a.ConvertToOpenAIVideo(task)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if strings.Contains(s, "internal.vendor.com") {
		t.Fatalf("upstream host leaked: %s", s)
	}
	if !strings.Contains(s, `"code":"asset_error"`) || !strings.Contains(s, "fetch") {
		t.Fatalf("error info lost: %s", s)
	}
	if !strings.Contains(s, `"id":"task_pub1"`) {
		t.Fatalf("public task id not set: %s", s)
	}
}

func TestConvertToOpenAIVideo_NoErrorUntouched(t *testing.T) {
	a := &TaskAdaptor{}
	task := &model.Task{
		TaskID: "task_pub2",
		Data:   []byte(`{"id":"video_up2","status":"completed","progress":100}`),
	}
	out, err := a.ConvertToOpenAIVideo(task)
	if err != nil {
		t.Fatal(err)
	}
	s := string(out)
	if !strings.Contains(s, `"status":"completed"`) || strings.Contains(s, "error") {
		t.Fatalf("success payload changed unexpectedly: %s", s)
	}
}

package commentconsole

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func materialImageDataURL(t *testing.T, format string) string {
	t.Helper()
	picture := image.NewRGBA(image.Rect(0, 0, 2, 2))
	picture.Set(0, 0, color.RGBA{R: 240, G: 80, B: 100, A: 255})
	picture.Set(1, 1, color.White)
	var encoded bytes.Buffer
	var err error
	switch format {
	case "png":
		err = png.Encode(&encoded, picture)
	case "jpeg":
		err = jpeg.Encode(&encoded, picture, nil)
	case "gif":
		err = gif.Encode(&encoded, picture, nil)
	default:
		t.Fatalf("未知的测试图片格式：%s", format)
	}
	if err != nil {
		t.Fatal(err)
	}
	return "data:image/" + format + ";base64," + base64.StdEncoding.EncodeToString(encoded.Bytes())
}

func TestMaterialImageSaveReplaceAndRemove(t *testing.T) {
	for _, format := range []string{"png", "jpeg"} {
		t.Run(format, func(t *testing.T) {
			e := testEngine(t, DemoBackend{})
			dataURL := materialImageDataURL(t, format)
			material := Material{
				Name: "图片参考素材", Category: "参考", Content: "评论前先确认对方预算。",
				Enabled: true, Image: dataURL,
			}
			if err := e.SaveMaterial(material); err != nil {
				t.Fatalf("保存有效 %s 图片失败：%v", format, err)
			}
			snapshot := e.Snapshot()
			count := len(snapshot.Materials)
			saved := snapshot.Materials[count-1]
			if saved.ID == "" || saved.Image != dataURL {
				t.Fatal("快照没有保留完整图片")
			}
			store := e.store.(*memoryStore)
			store.mu.Lock()
			persisted := clone(store.state)
			store.mu.Unlock()
			raw, err := json.Marshal(persisted)
			if err != nil {
				t.Fatal(err)
			}
			var decoded State
			if err := json.Unmarshal(raw, &decoded); err != nil {
				t.Fatal(err)
			}
			if decoded.Materials[count-1].Image != dataURL {
				t.Fatal("图片在存储与 JSON 序列化往返后发生变化")
			}

			replacementFormat := "png"
			if format == replacementFormat {
				replacementFormat = "jpeg"
			}
			replacement := materialImageDataURL(t, replacementFormat)
			saved.Image = replacement
			if err := e.SaveMaterial(saved); err != nil {
				t.Fatalf("替换素材图片失败：%v", err)
			}
			snapshot = e.Snapshot()
			if len(snapshot.Materials) != count || snapshot.Materials[count-1].Image != replacement {
				t.Fatal("替换图片新增了素材或未更新原记录")
			}
			saved.Image = ""
			if err := e.SaveMaterial(saved); err != nil {
				t.Fatalf("移除图片失败：%v", err)
			}
			snapshot = e.Snapshot()
			removed := snapshot.Materials[count-1]
			if removed.Image != "" || removed.Content != material.Content || len(snapshot.Materials) != count {
				t.Fatal("移除图片后内容或记录数量发生异常")
			}
			raw, err = json.Marshal(removed)
			if err != nil {
				t.Fatal(err)
			}
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(raw, &fields); err != nil {
				t.Fatal(err)
			}
			if _, exists := fields["image"]; exists {
				t.Fatal("已移除的图片字段应当省略")
			}
		})
	}
}

func TestMaterialImageRejectsInvalidInputWithoutChangingState(t *testing.T) {
	valid := materialImageDataURL(t, "png")
	payload := strings.SplitN(valid, ",", 2)[1]
	pngBytes, err := base64.StdEncoding.DecodeString(payload)
	if err != nil {
		t.Fatal(err)
	}
	large := append(append([]byte{}, pngBytes...), make([]byte, (512<<10)+1-len(pngBytes))...)
	brokenPNG := append([]byte{}, pngBytes...)
	// 保留有效 PNG 头，只破坏 IDAT 内容，确保校验覆盖完整解码。
	brokenPNG[41] ^= 0xff
	invalid := map[string]string{
		"外部URL":      "https://example.com/reference.png",
		"有效GIF也不支持":  materialImageDataURL(t, "gif"),
		"SVG":        "data:image/svg+xml;base64," + base64.StdEncoding.EncodeToString([]byte("<svg/>")),
		"其他MIME":     "data:text/plain;base64," + payload,
		"MIME不匹配":    "data:image/jpeg;base64," + payload,
		"缺少base64标记": "data:image/png," + payload,
		"非法base64":   "data:image/png;base64,@@@",
		"空图片":        "data:image/png;base64,",
		"损坏文件":       "data:image/png;base64," + base64.StdEncoding.EncodeToString([]byte("broken image")),
		"截断PNG":      "data:image/png;base64," + base64.StdEncoding.EncodeToString(pngBytes[:20]),
		"PNG内容损坏":    "data:image/png;base64," + base64.StdEncoding.EncodeToString(brokenPNG),
		"超过512KiB":   "data:image/png;base64," + base64.StdEncoding.EncodeToString(large),
	}
	for name, dataURL := range invalid {
		t.Run(name, func(t *testing.T) {
			e := testEngine(t, DemoBackend{})
			material := Material{Name: "原素材", Content: "原文字", Enabled: true, Image: valid}
			if err := e.SaveMaterial(material); err != nil {
				t.Fatal(err)
			}
			before := e.Snapshot()
			material = before.Materials[len(before.Materials)-1]
			material.Name = "不应保存的新名称"
			material.Content = "不应保存的新内容"
			material.Image = dataURL
			if err := e.SaveMaterial(material); err == nil {
				t.Fatal("应当拒绝非法图片")
			}
			beforeJSON, _ := json.Marshal(before)
			afterJSON, _ := json.Marshal(e.Snapshot())
			if !bytes.Equal(beforeJSON, afterJSON) {
				t.Fatal("图片校验失败后仍然改变了原状态")
			}
			material.ID = ""
			if err := e.SaveMaterial(material); err == nil {
				t.Fatal("新建素材也应校验图片")
			}
			afterJSON, _ = json.Marshal(e.Snapshot())
			if !bytes.Equal(beforeJSON, afterJSON) {
				t.Fatal("非法新素材被部分写入")
			}
		})
	}
}

func TestMaterialImagesAreExcludedFromModelReferences(t *testing.T) {
	dataURL := materialImageDataURL(t, "png")
	requests := make(chan []byte, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "cannot read test request", http.StatusBadRequest)
			return
		}
		requests <- body
		w.Header().Set("Content-Type", "application/json")
		decision, _ := json.Marshal(Decision{Match: true, Reason: "有明确的预算问题", Comment: "可以先明确预算。"})
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []any{map[string]any{"message": map[string]string{"content": string(decision)}}},
		})
	}))
	t.Cleanup(server.Close)
	materials := []Material{
		{Name: "文字参考", Category: "知识", Content: "先确认预算和需求。", Enabled: true, Image: dataURL},
		{Name: "已停用参考", Content: "disabled-reference-must-not-be-sent", Enabled: false, Image: materialImageDataURL(t, "gif")},
	}
	_, err := analyze(context.Background(),
		Model{Name: "测试模型", Model: "mock", BaseURL: server.URL},
		Task{Goal: "找到装修预算咨询"}, Post{Title: "装修预算怎么分配", Content: "预算有限，希望获得建议。"},
		Strategy{Mode: "ai", MaxLength: 120}, materials, "")
	if err != nil {
		t.Fatal(err)
	}
	body := <-requests
	if bytes.Contains(body, []byte("data:image/")) ||
		bytes.Contains(body, []byte(strings.SplitN(dataURL, ",", 2)[1])) {
		t.Fatal("图片或 base64 数据泄漏到了模型请求")
	}
	var envelope struct {
		Messages []struct {
			Role    string
			Content string
		}
	}
	if err := json.Unmarshal(body, &envelope); err != nil {
		t.Fatal(err)
	}
	var input struct {
		References []map[string]json.RawMessage
	}
	foundUserInput := false
	for _, message := range envelope.Messages {
		if message.Role == "user" {
			if err := json.Unmarshal([]byte(message.Content), &input); err != nil {
				t.Fatal(err)
			}
			foundUserInput = true
			if strings.Contains(message.Content, "disabled-reference-must-not-be-sent") {
				t.Fatal("停用素材被传入模型")
			}
		}
	}
	if !foundUserInput || len(input.References) != 1 {
		t.Fatalf("应仅传递一条启用的文字参考：%d", len(input.References))
	}
	for key := range input.References[0] {
		if strings.Contains(strings.ToLower(key), "image") {
			t.Fatal("模型参考素材中仍包含图片字段")
		}
	}
	var content string
	if err := json.Unmarshal(input.References[0]["content"], &content); err != nil || content != materials[0].Content {
		t.Fatal("剔除图片时丢失了文字参考")
	}
	if materials[0].Image != dataURL {
		t.Fatal("准备模型参考时修改了原素材的图片")
	}
}

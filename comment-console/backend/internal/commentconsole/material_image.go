package commentconsole

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"strings"
)

const maxMaterialImageBytes = 512 << 10

// 图片只作人工参考，限定格式和大小，避免存入外链或脚本。
func validateMaterialImage(value string) error {
	if value == "" {
		return nil
	}
	if len(value) > base64.StdEncoding.EncodedLen(maxMaterialImageBytes)+32 {
		return fmt.Errorf("图片附件不能超过 512 KiB")
	}
	header, payload, ok := strings.Cut(value, ",")
	formats := map[string]string{
		"data:image/png;base64":  "png",
		"data:image/jpeg;base64": "jpeg",
	}
	format, supported := formats[header]
	if !ok || !supported {
		return fmt.Errorf("图片参考附件仅支持静态 PNG 或 JPEG 文件，暂不支持 GIF")
	}
	data, err := base64.StdEncoding.DecodeString(payload)
	if err != nil || len(data) == 0 {
		return fmt.Errorf("图片附件编码无效")
	}
	if len(data) > maxMaterialImageBytes {
		return fmt.Errorf("图片附件不能超过 512 KiB")
	}
	config, actual, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || actual != format || config.Width <= 0 || config.Height <= 0 || config.Width > 8192 || config.Height > 8192 || int64(config.Width)*int64(config.Height) > 16000000 {
		return fmt.Errorf("图片附件格式或尺寸无效，最长边不能超过 8192 像素，总像素不能超过 1600 万")
	}
	if _, actual, err = image.Decode(bytes.NewReader(data)); err != nil || actual != format {
		return fmt.Errorf("图片附件损坏或格式无效")
	}
	return nil
}

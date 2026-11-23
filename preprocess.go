package main

import (
	"bytes"
	"context"
	"crypto/md5"
	"errors"
	"fmt"
	"image"
	_ "image/gif"
	"image/jpeg"
	"image/png"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode"

	"golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

var (
	// errNotImage 表示目标 URL 可访问，但返回内容不是受支持的图片格式。
	errNotImage = errors.New("响应不是图片")

	httpSchemePattern = regexp.MustCompile(`(?i)https?://`)
	htmlEntityTail    = regexp.MustCompile(`(?i)&(quot|apos|lt|gt);.*$`)
)

var referers = map[string]string{
	"chaoxing.com": "https://mooc1.chaoxing.com/", "xueyinonline.com": "https://mooc1.chaoxing.com/",
	"hnsyu.net": "https://mooc1.chaoxing.com/", "zhihuishu.com": "https://onlineweb.zhihuishu.com/",
	"icve.com.cn": "https://zjy2.icve.com.cn/", "icourse163.org": "https://www.icourse163.org/",
	"yuketang.cn": "https://www.yuketang.cn/",
}

var imageExtensions = map[string]bool{
	".png": true, ".jpg": true, ".jpeg": true, ".gif": true,
	".webp": true, ".bmp": true, ".avif": true, ".jfif": true,
}

var nonImageExtensions = map[string]bool{
	".html": true, ".htm": true, ".shtml": true, ".php": true, ".asp": true, ".aspx": true, ".jsp": true,
	".js": true, ".mjs": true, ".css": true, ".json": true, ".xml": true, ".txt": true, ".md": true,
	".pdf": true, ".doc": true, ".docx": true, ".xls": true, ".xlsx": true, ".ppt": true, ".pptx": true,
	".zip": true, ".rar": true, ".7z": true, ".tar": true, ".gz": true,
	".mp3": true, ".mp4": true, ".avi": true, ".mov": true, ".wav": true,
	".py": true, ".java": true, ".c": true, ".cpp": true, ".go": true, ".sh": true, ".sql": true,
	".yaml": true, ".yml": true, ".csv": true,
}

var visualContextKeywords = []string{
	"如图", "下图", "上图", "图中", "图示", "图形", "所示", "看图", "附图",
	"右图", "左图", "识图", "图表", "电路图", "流程图", "示意图", "曲线图", "结构图",
}

const imageUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 Chrome/125.0.0.0 Safari/537.36"

type imageRefKind int

const (
	// imageRefCandidateURL 表示带图片后缀但可能只是题目示例的公网链接，非图或 404 时可保留为文本。
	imageRefCandidateURL imageRefKind = iota
	// imageRefStrictURL 表示高度确认为题图的链接（如网课图片域名或看图/纯图上下文），失败时阻断作答。
	imageRefStrictURL
	// imageRefUploaded 表示 OCS 浏览器端上传的 [图片N] 占位符，必须处理成功。
	imageRefUploaded
)

type imageReference struct {
	raw  string
	kind imageRefKind
}

type ImageProcessor struct {
	dir   string
	http  *http.Client
	fetch func(context.Context, string) ([]byte, error)
}

func newImageProcessor(dir string) (*ImageProcessor, error) {
	if err := os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	p := &ImageProcessor{dir: dir}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.DialContext = func(ctx context.Context, network, address string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(address)
		if err != nil {
			return nil, err
		}
		ips, err := net.DefaultResolver.LookupIPAddr(ctx, host)
		if err != nil {
			return nil, err
		}
		for _, ip := range ips {
			if ip.IP.IsPrivate() || ip.IP.IsLoopback() || ip.IP.IsLinkLocalUnicast() || ip.IP.IsUnspecified() || ip.IP.IsMulticast() {
				continue
			}
			return (&net.Dialer{Timeout: 15 * time.Second}).DialContext(ctx, network, net.JoinHostPort(ip.IP.String(), port))
		}
		return nil, errors.New("图片地址不可访问")
	}
	p.http = &http.Client{Timeout: 30 * time.Second, Transport: transport, CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if len(via) >= 5 {
			return errors.New("图片跳转次数过多")
		}
		return validateImageURL(req.URL)
	}}
	p.fetch = p.download
	return p, nil
}

func validateImageURL(u *url.URL) error {
	if (u.Scheme != "https" && u.Scheme != "http") || u.Hostname() == "" || u.User != nil {
		return errors.New("图片地址无效")
	}
	host := strings.ToLower(u.Hostname())
	if host == "localhost" || strings.HasSuffix(host, ".localhost") {
		return errors.New("图片地址不可访问")
	}
	if ip := net.ParseIP(host); ip != nil && (ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsUnspecified() || ip.IsMulticast()) {
		return errors.New("图片地址不可访问")
	}
	return nil
}

func (p *ImageProcessor) download(ctx context.Context, raw string) ([]byte, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, err
	}
	if err := validateImageURL(u); err != nil {
		return nil, err
	}
	var last error
	var result []byte
	for attempt := 0; attempt < 3; attempt++ {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", imageUserAgent)
		req.Header.Set("Accept", "image/avif,image/webp,image/apng,image/*,*/*;q=0.8")
		for domain, ref := range referers {
			if u.Hostname() == domain || strings.HasSuffix(u.Hostname(), "."+domain) {
				req.Header.Set("Referer", ref)
				break
			}
		}
		resp, err := p.http.Do(req)
		if err != nil {
			last = err
		} else {
			func() {
				defer resp.Body.Close()
				if resp.StatusCode != http.StatusOK {
					last = fmt.Errorf("HTTP %d", resp.StatusCode)
					return
				}
				contentType := strings.ToLower(resp.Header.Get("Content-Type"))
				if !strings.HasPrefix(contentType, "image/") && !strings.Contains(contentType, "octet-stream") {
					last = errNotImage
					return
				}
				var data []byte
				data, last = io.ReadAll(io.LimitReader(resp.Body, 20<<20+1))
				if last == nil && len(data) > 20<<20 {
					last = errors.New("图片超过 20 MB")
				}
				if last == nil {
					if _, err := imageExtension(data); err != nil {
						last = fmt.Errorf("%w: %v", errNotImage, err)
						return
					}
				}
				if last == nil && len(data) > 5<<20 {
					data, last = shrinkImage(data)
				}
				if last == nil {
					result = data
				}
			}()
			if result != nil {
				return result, nil
			}
		}
		if last != nil && (errors.Is(last, errNotImage) || strings.Contains(last.Error(), "404") || strings.Contains(last.Error(), "403") || strings.Contains(last.Error(), "不可访问") || strings.Contains(last.Error(), "超过")) {
			break
		}
		if attempt < 2 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(time.Duration(attempt+1) * time.Second):
			}
		}
	}
	return nil, last
}

func shrinkImage(data []byte) ([]byte, error) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	if cfg.Width < 1 || cfg.Height < 1 || int64(cfg.Width)*int64(cfg.Height) > 16000000 {
		return nil, errors.New("图片尺寸无效")
	}
	original, format, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	width, height := cfg.Width, cfg.Height
	if width > 1024 || height > 1024 {
		if width >= height {
			height = max(1, height*1024/width)
			width = 1024
		} else {
			width = max(1, width*1024/height)
			height = 1024
		}
	}
	out := image.NewRGBA(image.Rect(0, 0, width, height))
	draw.CatmullRom.Scale(out, out.Bounds(), original, original.Bounds(), draw.Over, nil)
	var b bytes.Buffer
	if format == "jpeg" {
		err = jpeg.Encode(&b, out, &jpeg.Options{Quality: 85})
	} else {
		err = png.Encode(&b, out)
	}
	return b.Bytes(), err
}

func imageExtension(data []byte) (string, error) {
	switch http.DetectContentType(data) {
	case "image/jpeg":
		return ".jpg", nil
	case "image/png":
		return ".png", nil
	case "image/gif":
		return ".gif", nil
	case "image/webp":
		return ".webp", nil
	default:
		return "", errors.New("不支持的图片格式")
	}
}

func (p *ImageProcessor) save(raw string, data []byte) (string, error) {
	ext, err := imageExtension(data)
	if err != nil {
		return "", err
	}
	digest := md5.Sum([]byte(raw))
	if strings.HasPrefix(raw, "[图片") {
		// 同一编号在不同题目中对应不同图片，上传图片按内容缓存。
		digest = md5.Sum(data)
	}
	name := fmt.Sprintf("%x", digest)[:12] + ext
	filePath := filepath.Join(p.dir, name)
	if _, err = os.Stat(filePath); errors.Is(err, os.ErrNotExist) {
		err = os.WriteFile(filePath, data, 0600)
	}
	if err != nil {
		return "", err
	}
	return "/images/" + name, nil
}

type rawReferenceMatch struct {
	raw   string
	start int
	end   int
	isURL bool
}

// scanRawReferences 按原文顺序提取 [图片N] 与 http(s) 链接，正确切分紧邻的多 URL 及中英文/HTML 边界。
func scanRawReferences(text string) []rawReferenceMatch {
	var matches []rawReferenceMatch
	for _, loc := range imagePlaceholderPattern.FindAllStringIndex(text, -1) {
		matches = append(matches, rawReferenceMatch{
			raw: text[loc[0]:loc[1]], start: loc[0], end: loc[1], isURL: false,
		})
	}
	schemeLocs := httpSchemePattern.FindAllStringIndex(text, -1)
	for i, loc := range schemeLocs {
		start := loc[0]
		limit := len(text)
		if i+1 < len(schemeLocs) && schemeLocs[i+1][0] < limit {
			limit = schemeLocs[i+1][0]
		}
		end := start
		for end < limit {
			b := text[end]
			if b >= 0x80 || unicode.IsSpace(rune(b)) || strings.ContainsRune(`<>'"`+"`", rune(b)) {
				break
			}
			end++
		}
		raw := text[start:end]
		raw = htmlEntityTail.ReplaceAllString(raw, "")
		raw = trimURLTrailingPunctuation(raw)
		if raw != "" {
			matches = append(matches, rawReferenceMatch{
				raw: raw, start: start, end: start + len(raw), isURL: true,
			})
		}
	}
	// 按出现位置排序，保持题目与选项中原有图片顺序。
	for i := 1; i < len(matches); i++ {
		for j := i; j > 0 && matches[j].start < matches[j-1].start; j-- {
			matches[j], matches[j-1] = matches[j-1], matches[j]
		}
	}
	return matches
}

func trimURLTrailingPunctuation(raw string) string {
	for len(raw) > 0 {
		last := raw[len(raw)-1]
		switch last {
		case '.', ',', ';', ':', '!', '?', '"', '\'', '}':
			raw = raw[:len(raw)-1]
		case ')':
			if strings.Count(raw, "(") < strings.Count(raw, ")") {
				raw = raw[:len(raw)-1]
			} else {
				return raw
			}
		case ']':
			if strings.Count(raw, "[") < strings.Count(raw, "]") {
				raw = raw[:len(raw)-1]
			} else {
				return raw
			}
		default:
			return raw
		}
	}
	return raw
}

func isCourseDomain(host string) bool {
	host = strings.ToLower(host)
	for domain := range referers {
		if host == domain || strings.HasSuffix(host, "."+domain) {
			return true
		}
	}
	return false
}

func isKnownCourseImageURL(u *url.URL) bool {
	if !isCourseDomain(u.Hostname()) {
		return false
	}
	ext := strings.ToLower(path.Ext(u.Path))
	if nonImageExtensions[ext] {
		return false
	}
	if imageExtensions[ext] {
		return true
	}
	host := strings.ToLower(u.Hostname())
	for _, prefix := range []string{"p.", "img.", "pic.", "image.", "s1.", "s2."} {
		if strings.HasPrefix(host, prefix) {
			return true
		}
	}
	lowerPath := strings.ToLower(u.Path)
	return strings.Contains(lowerPath, "/star3/") || strings.Contains(lowerPath, "/origin/") ||
		strings.Contains(lowerPath, "/image") || strings.Contains(lowerPath, "/img")
}

func isCodeAttributeContext(text string, start int) bool {
	prefix := strings.TrimRight(text[:start], " \t\r\n")
	lower := strings.ToLower(prefix)
	for _, marker := range []string{
		`src="`, `src='`, `src=`, `src=&quot;`, `src=&apos;`,
		`href="`, `href='`, `href=`, `href=&quot;`, `href=&apos;`,
		`url(`, `url("`, `url('`,
	} {
		if strings.HasSuffix(lower, marker) {
			return true
		}
	}
	return false
}

func hasVisualContext(text string, urls []string) bool {
	remainder := text
	for _, u := range urls {
		remainder = strings.ReplaceAll(remainder, u, " ")
	}
	remainder = cleanTitle(remainder)
	trimmed := strings.Trim(remainder, " \t\r\n.。,，;；:：!！?？()（）[]【】_-—")
	if len([]rune(trimmed)) <= 6 {
		return true
	}
	for _, kw := range visualContextKeywords {
		if strings.Contains(remainder, kw) {
			return true
		}
	}
	return false
}

// extractCandidateImageReferences 提取文本中应尝试作为图片处理的引用，自动过滤普通网页/API/内网链接。
func extractCandidateImageReferences(text string) []imageReference {
	rawMatches := scanRawReferences(text)
	if len(rawMatches) == 0 {
		return nil
	}
	allURLs := make([]string, 0, len(rawMatches))
	for _, m := range rawMatches {
		if m.isURL {
			allURLs = append(allURLs, m.raw)
		}
	}
	visualCtx := hasVisualContext(text, allURLs)
	result := make([]imageReference, 0, len(rawMatches))
	for _, m := range rawMatches {
		if !m.isURL {
			result = append(result, imageReference{raw: m.raw, kind: imageRefUploaded})
			continue
		}
		u, err := url.Parse(m.raw)
		if err != nil || validateImageURL(u) != nil {
			// 本地/内网或非法地址在网课题目中属于题干文本本身，不作为图片下载。
			continue
		}
		ext := strings.ToLower(path.Ext(u.Path))
		if nonImageExtensions[ext] {
			continue
		}
		if isKnownCourseImageURL(u) {
			result = append(result, imageReference{raw: m.raw, kind: imageRefStrictURL})
			continue
		}
		if imageExtensions[ext] {
			kind := imageRefCandidateURL
			if visualCtx && !isCodeAttributeContext(text, m.start) {
				kind = imageRefStrictURL
			}
			result = append(result, imageReference{raw: m.raw, kind: kind})
		}
	}
	return result
}

// extractImageReferences 提取文本中的图片引用（供直传图片顺序对齐使用）。
func extractImageReferences(text string) []string {
	refs := extractCandidateImageReferences(text)
	result := make([]string, 0, len(refs))
	for _, ref := range refs {
		result = append(result, ref.raw)
	}
	return result
}

// captionTask 描述图片转述任务：使用转述模型把图片转换为文字。
type captionTask struct {
	run func(ctx context.Context, data []byte) (string, error)
}

// process 处理题目中的图片。普通非图片链接会原样保留在题干与选项中；
// 直传模式下保留原图数据交给回答模型；转述模式下用转述模型生成文字描述并替换图片引用。
func (p *ImageProcessor) process(ctx context.Context, title, options string, caption *captionTask, direct bool, inlineImages map[string][]byte) (string, string, []ImageInfo, map[string][]byte, error) {
	titleRefs := extractCandidateImageReferences(title)
	optionRefs := extractCandidateImageReferences(options)
	ordered := make([]imageReference, 0, len(titleRefs)+len(optionRefs))
	indexByRaw := make(map[string]int, len(titleRefs)+len(optionRefs))
	for _, ref := range append(titleRefs, optionRefs...) {
		if idx, exists := indexByRaw[ref.raw]; exists {
			if ref.kind > ordered[idx].kind {
				ordered[idx].kind = ref.kind
			}
			continue
		}
		indexByRaw[ref.raw] = len(ordered)
		ordered = append(ordered, ref)
	}

	infos := []ImageInfo{}
	images := map[string][]byte{}
	if len(ordered) == 0 {
		return title, options, infos, images, nil
	}

	var processErr error
	for _, ref := range ordered {
		raw := ref.raw
		info := ImageInfo{URL: raw, Status: "failed"}
		data, uploaded := inlineImages[raw]
		var err error
		if !uploaded {
			if ref.kind == imageRefUploaded {
				err = errors.New("缺少上传的图片数据")
			} else {
				data, err = p.fetch(ctx, raw)
				if err != nil {
					if shouldTreatURLAsPlainText(ref, err) {
						continue
					}
				}
			}
		}
		if err == nil && !direct && caption == nil {
			return title, options, infos, nil, errors.New("回答模型未标注图片能力，且未配置图片转述模型")
		}
		if err == nil {
			info.Src, err = p.save(raw, data)
		}
		if err == nil {
			images[raw] = data
			if direct {
				if uploaded {
					info.Description = "已接收"
				} else {
					info.Description = "已下载"
				}
			} else {
				info.Description, err = caption.run(ctx, data)
				info.Description = strings.TrimSpace(info.Description)
				if err == nil && info.Description == "" {
					err = errors.New("未识别")
				}
			}
		}
		if err != nil {
			info.Description = classifyImageError(err)
			processErr = errors.Join(processErr, fmt.Errorf("第 %d 张图片处理失败: %w", len(infos)+1, err))
		} else {
			info.Status = "done"
		}
		infos = append(infos, info)
		if !direct {
			title = strings.ReplaceAll(title, raw, "[图片: "+info.Description+"]")
			options = strings.ReplaceAll(options, raw, "[图片: "+info.Description+"]")
		}
	}
	return title, options, infos, images, processErr
}

// shouldTreatURLAsPlainText 判断下载失败的 URL 是否应降级为题目文本链接而非致命题图缺失。
func shouldTreatURLAsPlainText(ref imageReference, err error) bool {
	if ref.kind == imageRefUploaded {
		return false
	}
	if ref.kind == imageRefCandidateURL {
		return true
	}
	// 非网课域名的链接若明确返回网页/文本等非图片响应，视为普通文本链接。
	if errors.Is(err, errNotImage) {
		if u, parseErr := url.Parse(ref.raw); parseErr == nil && !isCourseDomain(u.Hostname()) {
			return true
		}
	}
	return false
}

func classifyImageError(err error) string {
	s := err.Error()
	switch {
	case strings.Contains(s, "404"):
		return "(图片已失效)"
	case strings.Contains(s, "403"):
		return "(图片拒绝访问)"
	case strings.Contains(s, "空白"):
		return "(空白)"
	default:
		return "(未识别)"
	}
}

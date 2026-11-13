package service

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const defaultGeminiBaseURL = "https://generativelanguage.googleapis.com/v1beta/models"

var geminiFallbackModels = []string{
	"gemini-3.6-flash",
	"gemini-3.8-flash",
	"gemini-3.7-flash",
	"gemini-3.5-flash",
	"gemini-3.1-flash-lite",
}

type QuizSource struct {
	Title       string
	Description string
	URL         string
	FileName    string
	MimeType    string
	FileBytes   []byte
}

type GeneratedQuestion struct {
	Prompt  string   `json:"prompt"`
	Options []string `json:"options"`
	Correct int      `json:"correct"`
}

type GeneratedQuiz struct {
	Title       string              `json:"title"`
	Description string              `json:"description"`
	Questions   []GeneratedQuestion `json:"questions"`
}

type QuizGenerator interface {
	GenerateQuiz(ctx context.Context, src QuizSource) (*GeneratedQuiz, error)
}

type GeminiQuizGenerator struct {
	apiKey     string
	model      string
	baseURL    string
	httpClient *http.Client
}

func NewGeminiQuizGenerator(apiKey, model string) *GeminiQuizGenerator {
	if strings.TrimSpace(model) == "" {
		model = geminiFallbackModels[0]
	}
	return &GeminiQuizGenerator{
		apiKey:  strings.TrimSpace(apiKey),
		model:   strings.TrimSpace(model),
		baseURL: defaultGeminiBaseURL,
		httpClient: &http.Client{
			Timeout: 90 * time.Second,
		},
	}
}

type geminiGenerateRequest struct {
	Contents         []geminiContent        `json:"contents"`
	GenerationConfig geminiGenerationConfig `json:"generationConfig"`
}

type geminiContent struct {
	Role  string       `json:"role,omitempty"`
	Parts []geminiPart `json:"parts"`
}

type geminiPart struct {
	Text       string            `json:"text,omitempty"`
	InlineData *geminiInlineData `json:"inlineData,omitempty"`
}

type geminiInlineData struct {
	MimeType string `json:"mimeType"`
	Data     string `json:"data"`
}

type geminiGenerationConfig struct {
	ResponseMimeType string  `json:"responseMimeType"`
	Temperature      float64 `json:"temperature"`
}

type geminiGenerateResponse struct {
	Candidates []struct {
		Content struct {
			Parts []struct {
				Text string `json:"text"`
			} `json:"parts"`
		} `json:"content"`
	} `json:"candidates"`
	Error *struct {
		Code    int    `json:"code"`
		Message string `json:"message"`
		Status  string `json:"status"`
	} `json:"error,omitempty"`
}

func (g *GeminiQuizGenerator) GenerateQuiz(ctx context.Context, src QuizSource) (*GeneratedQuiz, error) {
	if g.apiKey == "" {
		return nil, errors.New("GEMINI_API_KEY is not configured")
	}

	parts, err := buildQuizParts(src)
	if err != nil {
		return nil, err
	}

	models := g.orderedModels()
	var lastErr error
	for _, model := range models {
		for attempt := 0; attempt < 2; attempt++ {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			raw, statusCode, callErr := g.callGemini(ctx, model, parts)
			if callErr != nil {
				lastErr = callErr
				if statusCode == 404 || statusCode == 429 || statusCode >= 500 {
					break
				}
				continue
			}
			parsed, parseErr := ParseGeneratedQuizJSON(raw)
			if parseErr == nil {
				return parsed, nil
			}
			lastErr = parseErr
		}
	}
	if lastErr == nil {
		lastErr = errors.New("quiz generation failed")
	}
	return nil, lastErr
}

func (g *GeminiQuizGenerator) orderedModels() []string {
	seen := make(map[string]bool)
	var out []string
	if g.model != "" {
		seen[g.model] = true
		out = append(out, g.model)
	}
	for _, m := range geminiFallbackModels {
		if !seen[m] {
			seen[m] = true
			out = append(out, m)
		}
	}
	return out
}

func (g *GeminiQuizGenerator) callGemini(ctx context.Context, model string, parts []geminiPart) (string, int, error) {
	payload := geminiGenerateRequest{
		Contents: []geminiContent{
			{
				Role:  "user",
				Parts: parts,
			},
		},
		GenerationConfig: geminiGenerationConfig{
			ResponseMimeType: "application/json",
			Temperature:      0.4,
		},
	}

	bodyBytes, err := json.Marshal(payload)
	if err != nil {
		return "", 0, err
	}

	endpoint := fmt.Sprintf("%s/%s:generateContent?key=%s", strings.TrimRight(g.baseURL, "/"), model, g.apiKey)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(bodyBytes))
	if err != nil {
		return "", 0, err
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := g.httpClient.Do(req)
	if err != nil {
		return "", 0, err
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return "", resp.StatusCode, err
	}

	var parsed geminiGenerateResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return "", resp.StatusCode, fmt.Errorf("gemini invalid response (%d): %w", resp.StatusCode, err)
	}

	if resp.StatusCode >= 400 || parsed.Error != nil {
		msg := string(respBody)
		if parsed.Error != nil && parsed.Error.Message != "" {
			msg = parsed.Error.Message
		}
		return "", resp.StatusCode, fmt.Errorf("gemini %s error (%d): %s", model, resp.StatusCode, msg)
	}

	var sb strings.Builder
	for _, cand := range parsed.Candidates {
		for _, p := range cand.Content.Parts {
			sb.WriteString(p.Text)
		}
	}
	text := strings.TrimSpace(sb.String())
	if text == "" {
		return "", resp.StatusCode, errors.New("gemini returned empty response")
	}
	return text, resp.StatusCode, nil
}

func buildQuizParts(src QuizSource) ([]geminiPart, error) {
	var meta strings.Builder
	meta.WriteString("You are an expert educational assessment author.\n")
	meta.WriteString("Create a multiple-choice quiz in ENGLISH based on the lesson material below.\n")
	meta.WriteString("Strict rules:\n")
	meta.WriteString("1. Output ONLY valid JSON matching this exact object shape:\n")
	meta.WriteString(`{"title":"string","description":"string","questions":[{"prompt":"string","options":["A","B","C","D"],"correct":0}]}` + "\n")
	meta.WriteString("2. Write ALL text (title, description, every question prompt, and every option) in clear English.\n")
	meta.WriteString("3. Produce EXACTLY 10 questions in the \"questions\" array.\n")
	meta.WriteString("4. Each question MUST have EXACTLY 4 distinct non-empty answer choices in \"options\".\n")
	meta.WriteString("5. \"correct\" MUST be an integer 0, 1, 2, or 3 indicating the zero-based index of the single correct answer. Distribute correct indices across 0, 1, 2, 3.\n")
	meta.WriteString("6. Test real comprehension and key concepts from the material rather than trivia about file names.\n\n")
	meta.WriteString(fmt.Sprintf("Lesson material title: %s\n", strings.TrimSpace(src.Title)))
	if strings.TrimSpace(src.Description) != "" {
		meta.WriteString(fmt.Sprintf("Material description: %s\n", strings.TrimSpace(src.Description)))
	}
	if strings.TrimSpace(src.URL) != "" {
		meta.WriteString(fmt.Sprintf("Material URL: %s\n", strings.TrimSpace(src.URL)))
	}
	if strings.TrimSpace(src.FileName) != "" {
		meta.WriteString(fmt.Sprintf("Uploaded filename: %s\n", strings.TrimSpace(src.FileName)))
	}

	parts := []geminiPart{}
	if len(src.FileBytes) > 0 {
		inlinePart, extractedText, extractErr := extractSourceContent(src.FileName, src.MimeType, src.FileBytes)
		if extractErr != nil && strings.TrimSpace(src.URL) == "" && strings.TrimSpace(src.Description) == "" {
			return nil, extractErr
		}
		if inlinePart != nil {
			parts = append(parts, *inlinePart)
		}
		if strings.TrimSpace(extractedText) != "" {
			meta.WriteString("\nExtracted lesson material content:\n")
			meta.WriteString(limitString(extractedText, 60000))
			meta.WriteString("\n")
		}
	} else if strings.TrimSpace(src.URL) == "" && strings.TrimSpace(src.Description) == "" && strings.TrimSpace(src.Title) == "" {
		return nil, errors.New("material has no readable content to generate a quiz from")
	}

	parts = append([]geminiPart{{Text: meta.String()}}, parts...)
	return parts, nil
}

func extractSourceContent(fileName, mimeType string, data []byte) (*geminiPart, string, error) {
	ext := strings.ToLower(filepath.Ext(fileName))
	mime := strings.ToLower(strings.TrimSpace(mimeType))

	if ext == ".pdf" || mime == "application/pdf" {
		if len(data) > 18*1024*1024 {
			return nil, "", errors.New("PDF exceeds 18MB limit for AI quiz generation")
		}
		return &geminiPart{
			InlineData: &geminiInlineData{
				MimeType: "application/pdf",
				Data:     base64.StdEncoding.EncodeToString(data),
			},
		}, "", nil
	}

	if imageMime := resolveImageMime(ext, mime); imageMime != "" {
		if len(data) > 18*1024*1024 {
			return nil, "", errors.New("image exceeds 18MB limit for AI quiz generation")
		}
		return &geminiPart{
			InlineData: &geminiInlineData{
				MimeType: imageMime,
				Data:     base64.StdEncoding.EncodeToString(data),
			},
		}, "", nil
	}

	if ext == ".pptx" || strings.Contains(mime, "presentationml") {
		text, err := ExtractPPTXText(data)
		if err != nil {
			return nil, "", err
		}
		return nil, text, nil
	}

	if ext == ".docx" || strings.Contains(mime, "wordprocessingml") {
		text, err := ExtractDOCXText(data)
		if err != nil {
			return nil, "", err
		}
		return nil, text, nil
	}

	if ext == ".zip" || mime == "application/zip" {
		text, err := ExtractZipText(data)
		if err != nil {
			return nil, "", err
		}
		return nil, text, nil
	}

	if isLikelyText(ext, mime, data) {
		return nil, string(data), nil
	}

	fallback := extractPrintableRuns(data)
	if len(strings.Fields(fallback)) >= 12 {
		return nil, fallback, nil
	}

	return nil, "", fmt.Errorf("unsupported material format for AI extraction: %s", fileName)
}

func resolveImageMime(ext, mime string) string {
	switch ext {
	case ".png":
		return "image/png"
	case ".jpg", ".jpeg":
		return "image/jpeg"
	case ".gif":
		return "image/gif"
	case ".webp":
		return "image/webp"
	}
	if strings.HasPrefix(mime, "image/") {
		return mime
	}
	return ""
}

func isLikelyText(ext, mime string, data []byte) bool {
	switch ext {
	case ".txt", ".md", ".markdown", ".csv", ".tsv", ".json", ".yaml", ".yml", ".xml", ".html", ".htm",
		".go", ".ts", ".tsx", ".js", ".jsx", ".py", ".java", ".c", ".cpp", ".h", ".sql", ".sh", ".css", ".rst":
		return true
	}
	if strings.HasPrefix(mime, "text/") || mime == "application/json" || mime == "application/xml" {
		return true
	}
	if len(data) == 0 || !utf8.Valid(data) {
		return false
	}
	sample := data
	if len(sample) > 4096 {
		sample = sample[:4096]
	}
	control := 0
	for _, r := range string(sample) {
		if r < 32 && r != '\n' && r != '\r' && r != '\t' {
			control++
		}
	}
	return control == 0
}

var slideNumberPattern = regexp.MustCompile(`(\d+)\.xml$`)

func ExtractPPTXText(data []byte) (string, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", fmt.Errorf("failed to open pptx archive: %w", err)
	}

	type slideFile struct {
		num  int
		file *zip.File
	}
	var slides []slideFile
	for _, f := range zr.File {
		if strings.HasPrefix(f.Name, "ppt/slides/slide") && strings.HasSuffix(f.Name, ".xml") {
			num := 0
			if m := slideNumberPattern.FindStringSubmatch(f.Name); len(m) == 2 {
				num, _ = strconv.Atoi(m[1])
			}
			slides = append(slides, slideFile{num: num, file: f})
		}
	}
	sort.Slice(slides, func(i, j int) bool { return slides[i].num < slides[j].num })

	var out strings.Builder
	for _, s := range slides {
		text, err := readZipXMLText(s.file)
		if err != nil || strings.TrimSpace(text) == "" {
			continue
		}
		out.WriteString(fmt.Sprintf("Slide %d:\n%s\n\n", s.num, strings.TrimSpace(text)))
	}
	result := strings.TrimSpace(out.String())
	if result == "" {
		return "", errors.New("no readable text found in pptx slides")
	}
	return result, nil
}

func ExtractDOCXText(data []byte) (string, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", fmt.Errorf("failed to open docx archive: %w", err)
	}

	var out strings.Builder
	for _, f := range zr.File {
		if f.Name == "word/document.xml" || strings.HasPrefix(f.Name, "word/header") || strings.HasPrefix(f.Name, "word/footer") {
			text, readErr := readZipXMLText(f)
			if readErr == nil && strings.TrimSpace(text) != "" {
				out.WriteString(strings.TrimSpace(text))
				out.WriteString("\n\n")
			}
		}
	}
	result := strings.TrimSpace(out.String())
	if result == "" {
		return "", errors.New("no readable text found in docx document")
	}
	return result, nil
}

func ExtractZipText(data []byte) (string, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", fmt.Errorf("failed to open zip archive: %w", err)
	}
	var out strings.Builder
	for _, f := range zr.File {
		if f.FileInfo().IsDir() || f.UncompressedSize64 > 2*1024*1024 {
			continue
		}
		rc, openErr := f.Open()
		if openErr != nil {
			continue
		}
		b, readErr := io.ReadAll(io.LimitReader(rc, 512*1024))
		_ = rc.Close()
		if readErr != nil || len(b) == 0 {
			continue
		}
		ext := strings.ToLower(filepath.Ext(f.Name))
		switch ext {
		case ".pptx":
			if t, e := ExtractPPTXText(b); e == nil {
				out.WriteString(fmt.Sprintf("[%s]\n%s\n\n", f.Name, t))
			}
		case ".docx":
			if t, e := ExtractDOCXText(b); e == nil {
				out.WriteString(fmt.Sprintf("[%s]\n%s\n\n", f.Name, t))
			}
		default:
			if isLikelyText(ext, "", b) {
				out.WriteString(fmt.Sprintf("[%s]\n%s\n\n", f.Name, string(b)))
			}
		}
		if out.Len() >= 60000 {
			break
		}
	}
	res := strings.TrimSpace(out.String())
	if res == "" {
		return "", errors.New("no readable text files found in zip archive")
	}
	return res, nil
}

func readZipXMLText(f *zip.File) (string, error) {
	rc, err := f.Open()
	if err != nil {
		return "", err
	}
	defer rc.Close()

	dec := xml.NewDecoder(rc)
	var out strings.Builder
	for {
		tok, tokErr := dec.Token()
		if tokErr != nil {
			if tokErr == io.EOF {
				break
			}
			return out.String(), tokErr
		}
		switch t := tok.(type) {
		case xml.CharData:
			s := strings.TrimSpace(string(t))
			if s != "" {
				if out.Len() > 0 {
					last := out.String()[out.Len()-1]
					if last != '\n' && last != ' ' {
						out.WriteByte(' ')
					}
				}
				out.WriteString(s)
			}
		case xml.EndElement:
			if t.Name.Local == "p" || t.Name.Local == "br" {
				out.WriteByte('\n')
			}
		}
	}
	return out.String(), nil
}

func extractPrintableRuns(data []byte) string {
	var runs []string
	var cur strings.Builder
	flush := func() {
		if cur.Len() >= 4 {
			runs = append(runs, cur.String())
		}
		cur.Reset()
	}
	for _, b := range data {
		if b >= 32 && b < 127 {
			cur.WriteByte(b)
		} else if unicode.IsSpace(rune(b)) {
			if cur.Len() > 0 && cur.String()[cur.Len()-1] != ' ' {
				cur.WriteByte(' ')
			}
		} else {
			flush()
		}
		if len(runs) > 500 {
			break
		}
	}
	flush()
	return strings.Join(runs, " ")
}

func limitString(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	return s[:maxBytes]
}

func ParseGeneratedQuizJSON(raw string) (*GeneratedQuiz, error) {
	cleaned := strings.TrimSpace(raw)
	if strings.HasPrefix(cleaned, "```") {
		cleaned = strings.TrimPrefix(cleaned, "```json")
		cleaned = strings.TrimPrefix(cleaned, "```JSON")
		cleaned = strings.TrimPrefix(cleaned, "```")
		if idx := strings.LastIndex(cleaned, "```"); idx >= 0 {
			cleaned = cleaned[:idx]
		}
		cleaned = strings.TrimSpace(cleaned)
	}

	start := strings.IndexByte(cleaned, '{')
	end := strings.LastIndexByte(cleaned, '}')
	if start >= 0 && end > start {
		cleaned = cleaned[start : end+1]
	}

	var parsed GeneratedQuiz
	if err := json.Unmarshal([]byte(cleaned), &parsed); err != nil {
		return nil, fmt.Errorf("invalid quiz JSON from AI: %w", err)
	}

	parsed.Title = strings.TrimSpace(parsed.Title)
	parsed.Description = strings.TrimSpace(parsed.Description)

	if len(parsed.Questions) < 10 {
		return nil, fmt.Errorf("expected 10 questions from AI, got %d", len(parsed.Questions))
	}
	if len(parsed.Questions) > 10 {
		parsed.Questions = parsed.Questions[:10]
	}

	valid := make([]GeneratedQuestion, 0, 10)
	for i, q := range parsed.Questions {
		prompt := strings.TrimSpace(q.Prompt)
		if prompt == "" {
			return nil, fmt.Errorf("question %d has empty prompt", i+1)
		}
		opts := make([]string, 0, len(q.Options))
		for _, opt := range q.Options {
			trimmed := strings.TrimSpace(opt)
			if trimmed != "" {
				opts = append(opts, trimmed)
			}
		}
		if len(opts) != 4 {
			return nil, fmt.Errorf("question %d must have 4 options, got %d", i+1, len(opts))
		}
		if q.Correct < 0 || q.Correct >= len(opts) {
			return nil, fmt.Errorf("question %d has invalid correct index %d", i+1, q.Correct)
		}
		valid = append(valid, GeneratedQuestion{
			Prompt:  prompt,
			Options: opts,
			Correct: q.Correct,
		})
	}
	parsed.Questions = valid
	return &parsed, nil
}

// Image MetaHub 日本語化パッチャー (v0.23.0 用)
// app.asar 内の dist/*.js の UI 文字列を辞書で置換して再パックする。
package main

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

//go:embed dict.json
var dictJSON []byte

// 検証済みバージョン一覧。
// 本体がバージョンアップしたら、動作確認が取れたバージョンをここに1行追加する。
// 一覧に無いバージョンでも、警告を出した上でそのまま日本語化を試せる
// （文字列が一致しなければ置換0件で自動的に中止するので安全）。
var testedVersions = []string{
	"0.23.0",
}

func isTestedVersion(v string) bool {
	for _, t := range testedVersions {
		if t == v {
			return true
		}
	}
	return false
}

// ---------- ordered JSON ----------
type Node struct {
	IsObj   bool
	IsArr   bool
	Pairs   []Pair
	Items   []*Node
	Scalar  json.RawMessage
}
type Pair struct {
	Key string
	Val *Node
}

func parseJSON(data []byte) (*Node, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	return parseValue(dec)
}

func parseValue(dec *json.Decoder) (*Node, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	switch t := tok.(type) {
	case json.Delim:
		switch t {
		case '{':
			n := &Node{IsObj: true}
			for dec.More() {
				kt, err := dec.Token()
				if err != nil {
					return nil, err
				}
				key, _ := kt.(string)
				v, err := parseValue(dec)
				if err != nil {
					return nil, err
				}
				n.Pairs = append(n.Pairs, Pair{key, v})
			}
			_, err := dec.Token()
			return n, err
		case '[':
			n := &Node{IsArr: true}
			for dec.More() {
				v, err := parseValue(dec)
				if err != nil {
					return nil, err
				}
				n.Items = append(n.Items, v)
			}
			_, err := dec.Token()
			return n, err
		}
	case string:
		b, _ := json.Marshal(t)
		return &Node{Scalar: b}, nil
	case json.Number:
		return &Node{Scalar: json.RawMessage(t.String())}, nil
	case bool:
		if t {
			return &Node{Scalar: json.RawMessage("true")}, nil
		}
		return &Node{Scalar: json.RawMessage("false")}, nil
	case nil:
		return &Node{Scalar: json.RawMessage("null")}, nil
	}
	return nil, fmt.Errorf("unexpected token %v", tok)
}

func (n *Node) marshal(buf *bytes.Buffer) {
	if n.IsObj {
		buf.WriteByte('{')
		for i, p := range n.Pairs {
			if i > 0 {
				buf.WriteByte(',')
			}
			kb, _ := json.Marshal(p.Key)
			buf.Write(kb)
			buf.WriteByte(':')
			p.Val.marshal(buf)
		}
		buf.WriteByte('}')
		return
	}
	if n.IsArr {
		buf.WriteByte('[')
		for i, it := range n.Items {
			if i > 0 {
				buf.WriteByte(',')
			}
			it.marshal(buf)
		}
		buf.WriteByte(']')
		return
	}
	buf.Write(n.Scalar)
}

func (n *Node) get(key string) *Node {
	if n == nil || !n.IsObj {
		return nil
	}
	for _, p := range n.Pairs {
		if p.Key == key {
			return p.Val
		}
	}
	return nil
}

func (n *Node) set(key string, v *Node) {
	for i, p := range n.Pairs {
		if p.Key == key {
			n.Pairs[i].Val = v
			return
		}
	}
	n.Pairs = append(n.Pairs, Pair{key, v})
}

func strNode(s string) *Node {
	b, _ := json.Marshal(s)
	return &Node{Scalar: b}
}
func numNode(v int64) *Node {
	return &Node{Scalar: json.RawMessage(strconv.FormatInt(v, 10))}
}
func (n *Node) asString() string {
	if n == nil || n.IsObj || n.IsArr {
		return ""
	}
	var s string
	if err := json.Unmarshal(n.Scalar, &s); err == nil {
		return s
	}
	return string(n.Scalar)
}
func (n *Node) asInt() int64 {
	if n == nil {
		return 0
	}
	v, _ := strconv.ParseInt(string(n.Scalar), 10, 64)
	return v
}

// ---------- asar ----------
type FileEntry struct {
	Path string
	Node *Node
	Offset int64
	Size   int64
}

func collectFiles(n *Node, prefix string, out *[]*FileEntry) {
	files := n.get("files")
	if files == nil {
		return
	}
	for _, p := range files.Pairs {
		path := p.Key
		if prefix != "" {
			path = prefix + "/" + p.Key
		}
		if p.Val.get("files") != nil {
			collectFiles(p.Val, path, out)
		} else if p.Val.get("link") != nil {
			// symlink: header only
		} else if p.Val.get("unpacked") != nil && p.Val.get("offset") == nil {
			// unpacked: header only, no data in archive
		} else if p.Val.get("offset") != nil {
			off, _ := strconv.ParseInt(p.Val.get("offset").asString(), 10, 64)
			*out = append(*out, &FileEntry{Path: path, Node: p.Val, Offset: off, Size: p.Val.get("size").asInt()})
		}
	}
}

func integrity(data []byte) *Node {
	const blockSize = 4 * 1024 * 1024
	sum := sha256.Sum256(data)
	blocks := &Node{IsArr: true, Items: []*Node{}}
	for off := 0; off < len(data); off += blockSize {
		end := off + blockSize
		if end > len(data) {
			end = len(data)
		}
		b := sha256.Sum256(data[off:end])
		blocks.Items = append(blocks.Items, strNode(hex.EncodeToString(b[:])))
	}
	obj := &Node{IsObj: true}
	obj.set("algorithm", strNode("SHA256"))
	obj.set("hash", strNode(hex.EncodeToString(sum[:])))
	obj.set("blockSize", numNode(blockSize))
	obj.set("blocks", blocks)
	return obj
}

// UI表示位置のパターンだけを置換する（内部キーやKeyboardEvent名などを壊さないため）
var uiPrefixes = []string{
	"children:", "children:[",
	"label:", "title:", "placeholder:",
	"description:", "heading:", "tooltip:", "message:", "subtitle:",
	"\"aria-label\":", "ariaLabel:",
	"emptyMessage:", "confirmText:", "cancelText:",
}

// Tier B（値の位置でも置換してよいキー）: 複数語の文は内部キーになりにくい。
// 単語の場合は動作系の表示ラベルのみ許可する。
var tierBWords = map[string]bool{
	"Enable": true, "Disable": true, "Create": true, "Delete": true,
	"Save": true, "Cancel": true, "Remove": true, "Rename": true,
	"Export": true, "Generate": true, "Pause": true, "Resume": true,
	"Play": true, "Stop": true, "Search": true, "Clear": true, "Back": true,
	"Dismiss": true, "Copy": true,
}

func isTierB(k string) bool {
	if strings.Contains(k, " ") || strings.HasSuffix(k, "...") || strings.HasSuffix(k, "…") || strings.HasSuffix(k, ".") || strings.HasSuffix(k, "?") {
		return true
	}
	return tierBWords[k]
}

// ライブラリ・ワーカー系バンドルは対象外
var excludeSubstrings = []string{
	"three.module", "Worker", "Loader", "Exporter", "OrbitControls",
	"clusteringEngine", "autoTagging", "boardVisualGrouping", "relatedImageSearch",
}

func isPatchTarget(path string) bool {
	if !strings.HasPrefix(path, "dist/") || !strings.HasSuffix(path, ".js") {
		return false
	}
	for _, ex := range excludeSubstrings {
		if strings.Contains(path, ex) {
			return false
		}
	}
	return true
}

func escapeJS(s string, q byte) string {
	var b strings.Builder
	for _, r := range s {
		switch {
		case r == rune(q):
			b.WriteByte('\\')
			b.WriteRune(r)
		case r == '\\':
			b.WriteString("\\\\")
		case r == '\n':
			b.WriteString("\\n")
		case r == '\r':
			b.WriteString("\\r")
		case r == '\t':
			b.WriteString("\\t")
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}

func repack(srcPath, dstPath string, dict map[string]string, hitCount map[string]int) (int, error) {
	f, err := os.Open(srcPath)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	pre := make([]byte, 8)
	if _, err := io.ReadFull(f, pre); err != nil {
		return 0, err
	}
	headerBufLen := int64(binary.LittleEndian.Uint32(pre[4:8]))
	headerBuf := make([]byte, headerBufLen)
	if _, err := io.ReadFull(f, headerBuf); err != nil {
		return 0, err
	}
	jsonLen := int64(binary.LittleEndian.Uint32(headerBuf[4:8]))
	header, err := parseJSON(headerBuf[8 : 8+jsonLen])
	if err != nil {
		return 0, fmt.Errorf("header parse: %w", err)
	}
	dataStart := int64(8) + headerBufLen

	var entries []*FileEntry
	collectFiles(header, "", &entries)
	ordered := make([]*FileEntry, len(entries))
	copy(ordered, entries)
	sort.SliceStable(ordered, func(i, j int) bool { return ordered[i].Offset < ordered[j].Offset })

	// keys sorted by length desc for stable behaviour
	keys := make([]string, 0, len(dict))
	for k := range dict {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return len(keys[i]) > len(keys[j]) })

	totalReplacements := 0
	newData := make(map[*FileEntry][]byte)
	var newOffset int64
	for _, e := range ordered {
		buf := make([]byte, e.Size)
		if e.Size > 0 {
			if _, err := f.ReadAt(buf, dataStart+e.Offset); err != nil {
				return 0, fmt.Errorf("read %s: %w", e.Path, err)
			}
		}
		if isPatchTarget(e.Path) {
			s := string(buf)
			for _, k := range keys {
				ja := dict[k]
				for _, q := range []byte{'"', '\''} {
					qs := string(q)
					en := escapeJS(k, q)
					jp := escapeJS(ja, q)
					// Tier A: 表示プロパティの値として現れる場合
					for _, prefix := range uiPrefixes {
						needle := prefix + qs + en + qs
						c := strings.Count(s, needle)
						if c > 0 {
							s = strings.ReplaceAll(s, needle, prefix+qs+jp+qs)
							hitCount[k] += c
							totalReplacements += c
						}
					}
					// Tier B: 三項演算子・配列要素・関数引数（複数語の文と動作系の単語のみ）
					if isTierB(k) {
						for _, term := range []string{",", "}", ")", "]"} {
							needle := ":" + qs + en + qs + term
							c := strings.Count(s, needle)
							if c > 0 {
								s = strings.ReplaceAll(s, needle, ":"+qs+jp+qs+term)
								hitCount[k] += c
								totalReplacements += c
							}
							needle = "," + qs + en + qs + term
							c = strings.Count(s, needle)
							if c > 0 {
								s = strings.ReplaceAll(s, needle, ","+qs+jp+qs+term)
								hitCount[k] += c
								totalReplacements += c
							}
							needle = "(" + qs + en + qs + term
							c = strings.Count(s, needle)
							if c > 0 {
								s = strings.ReplaceAll(s, needle, "("+qs+jp+qs+term)
								hitCount[k] += c
								totalReplacements += c
							}
						}
						needle := "?" + qs + en + qs + ":"
						c := strings.Count(s, needle)
						if c > 0 {
							s = strings.ReplaceAll(s, needle, "?"+qs+jp+qs+":")
							hitCount[k] += c
							totalReplacements += c
						}
					}
				}
			}
			buf = []byte(s)
		}
		newData[e] = buf
		e.Node.set("size", numNode(int64(len(buf))))
		e.Node.set("offset", strNode(strconv.FormatInt(newOffset, 10)))
		e.Node.set("integrity", integrity(buf))
		newOffset += int64(len(buf))
	}

	// serialize header
	var hb bytes.Buffer
	header.marshal(&hb)
	jsonBytes := hb.Bytes()
	padded := (len(jsonBytes) + 3) &^ 3
	payload := make([]byte, 4+padded)
	binary.LittleEndian.PutUint32(payload[0:4], uint32(len(jsonBytes)))
	copy(payload[4:], jsonBytes)
	headerPickle := make([]byte, 4+len(payload))
	binary.LittleEndian.PutUint32(headerPickle[0:4], uint32(len(payload)))
	copy(headerPickle[4:], payload)
	sizePickle := make([]byte, 8)
	binary.LittleEndian.PutUint32(sizePickle[0:4], 4)
	binary.LittleEndian.PutUint32(sizePickle[4:8], uint32(len(headerPickle)))

	out, err := os.Create(dstPath)
	if err != nil {
		return 0, err
	}
	defer out.Close()
	w := bufio.NewWriterSize(out, 1<<20)
	if _, err := w.Write(sizePickle); err != nil {
		return 0, err
	}
	if _, err := w.Write(headerPickle); err != nil {
		return 0, err
	}
	for _, e := range ordered {
		if _, err := w.Write(newData[e]); err != nil {
			return 0, err
		}
	}
	if err := w.Flush(); err != nil {
		return 0, err
	}
	return totalReplacements, nil
}

func readPackageVersion(asarPath string) string {
	f, err := os.Open(asarPath)
	if err != nil {
		return ""
	}
	defer f.Close()
	pre := make([]byte, 8)
	if _, err := io.ReadFull(f, pre); err != nil {
		return ""
	}
	headerBufLen := int64(binary.LittleEndian.Uint32(pre[4:8]))
	headerBuf := make([]byte, headerBufLen)
	if _, err := io.ReadFull(f, headerBuf); err != nil {
		return ""
	}
	jsonLen := int64(binary.LittleEndian.Uint32(headerBuf[4:8]))
	header, err := parseJSON(headerBuf[8 : 8+jsonLen])
	if err != nil {
		return ""
	}
	var entries []*FileEntry
	collectFiles(header, "", &entries)
	dataStart := int64(8) + headerBufLen
	for _, e := range entries {
		if e.Path == "package.json" {
			buf := make([]byte, e.Size)
			if _, err := f.ReadAt(buf, dataStart+e.Offset); err != nil {
				return ""
			}
			var pkg struct {
				Version string `json:"version"`
			}
			if err := json.Unmarshal(buf, &pkg); err == nil {
				return pkg.Version
			}
		}
	}
	return ""
}

func containsBytes(path string, needle []byte) bool {
	f, err := os.Open(path)
	if err != nil {
		return false
	}
	defer f.Close()
	buf := make([]byte, 1<<20)
	var tail []byte
	for {
		n, err := f.Read(buf)
		if n > 0 {
			chunk := append(append([]byte{}, tail...), buf[:n]...)
			if bytes.Contains(chunk, needle) {
				return true
			}
			if len(chunk) > len(needle) {
				tail = chunk[len(chunk)-len(needle):]
			} else {
				tail = chunk
			}
		}
		if err != nil {
			return false
		}
	}
}

func loadDict() (map[string]string, error) {
	var d map[string]string
	if err := json.Unmarshal(dictJSON, &d); err != nil {
		return nil, err
	}
	return d, nil
}

func main() {
	setConsoleUTF8()
	args := os.Args[1:]

	// non-interactive test mode: --apply --asar X --out Y
	testMode := false
	var asarArg, outArg string
	for i := 0; i < len(args); i++ {
		if args[i] == "--apply" {
			testMode = true
		}
		if args[i] == "--asar" && i+1 < len(args) {
			asarArg = args[i+1]
		}
		if args[i] == "--out" && i+1 < len(args) {
			outArg = args[i+1]
		}
	}
	dict, err := loadDict()
	if err != nil {
		fmt.Println("辞書の読み込みに失敗しました:", err)
		pause()
		os.Exit(1)
	}

	if testMode {
		hit := map[string]int{}
		n, err := repack(asarArg, outArg, dict, hit)
		if err != nil {
			fmt.Println("error:", err)
			os.Exit(1)
		}
		fmt.Printf("replacements=%d\n", n)
		missed := []string{}
		for k := range dict {
			if hit[k] == 0 {
				missed = append(missed, k)
			}
		}
		sort.Strings(missed)
		fmt.Printf("missed=%d\n", len(missed))
		for _, m := range missed {
			fmt.Println("MISS:", m)
		}
		return
	}

	fmt.Println("=== Image MetaHub 日本語化パッチ ===")
	fmt.Println("検証済みバージョン:", strings.Join(testedVersions, ", "))
	fmt.Println()

	// locate app.asar
	var asarPath string
	for _, a := range args {
		if strings.HasSuffix(strings.ToLower(a), ".asar") {
			asarPath = a
		} else if st, err := os.Stat(a); err == nil && st.IsDir() {
			for _, cand := range []string{filepath.Join(a, "app.asar"), filepath.Join(a, "resources", "app.asar")} {
				if _, err := os.Stat(cand); err == nil {
					asarPath = cand
				}
			}
		}
	}
	if asarPath == "" {
		candidates := []string{
			"app.asar",
			filepath.Join("resources", "app.asar"),
		}
		if lad := os.Getenv("LOCALAPPDATA"); lad != "" {
			candidates = append(candidates, filepath.Join(lad, "Programs", "Image MetaHub", "resources", "app.asar"))
		}
		if pf := os.Getenv("ProgramFiles"); pf != "" {
			candidates = append(candidates, filepath.Join(pf, "Image MetaHub", "resources", "app.asar"))
		}
		exe, _ := os.Executable()
		if exe != "" {
			d := filepath.Dir(exe)
			candidates = append(candidates,
				filepath.Join(d, "app.asar"),
				filepath.Join(d, "resources", "app.asar"),
				filepath.Join(d, "..", "resources", "app.asar"))
		}
		for _, c := range candidates {
			if _, err := os.Stat(c); err == nil {
				abs, _ := filepath.Abs(c)
				asarPath = abs
				break
			}
		}
	}
	if asarPath == "" {
		fmt.Println("app.asar が見つかりませんでした。")
		fmt.Println("このexeを Image MetaHub のインストールフォルダ内の resources フォルダに置いて実行するか、")
		fmt.Println("app.asar をこのexeにドラッグ＆ドロップしてください。")
		pause()
		os.Exit(1)
	}
	fmt.Println("対象:", asarPath)

	ver := readPackageVersion(asarPath)
	fmt.Println("バージョン:", ver)
	if ver == "" {
		fmt.Println("バージョンが読み取れませんでした。中止します。")
		pause()
		os.Exit(1)
	}
	if !isTestedVersion(ver) {
		fmt.Println("注意: このバージョンは未検証です。")
		fmt.Println("文字列が一致すればそのまま日本語化できますが、新しく増えた画面は英語のまま残ることがあります。")
		fmt.Println("置換できる文字列が0件の場合は、書き換えずに自動で中止します。")
		fmt.Print("このまま続けますか？ [y/N]: ")
		ans := readLine()
		if strings.ToLower(strings.TrimSpace(ans)) != "y" {
			fmt.Println("中止しました。")
			pause()
			return
		}
	}

	backupPath := asarPath + ".bak"
	marker := []byte("プロンプト、モデルなどで検索")
	already := containsBytes(asarPath, marker)

	if already {
		fmt.Println("すでに日本語化されています。")
		if _, err := os.Stat(backupPath); err != nil {
			fmt.Println("バックアップが無いので元に戻せません。")
			pause()
			return
		}
		fmt.Print("元の英語版に戻しますか？ [y/N]: ")
		ans := readLine()
		if strings.ToLower(strings.TrimSpace(ans)) == "y" {
			if err := copyFile(backupPath, asarPath); err != nil {
				fmt.Println("復元に失敗しました:", err)
			} else {
				fmt.Println("英語版に戻しました。")
			}
		}
		pause()
		return
	}

	fmt.Print("日本語化します。Enterで開始、やめるときは Ctrl+C: ")
	readLine()

	if _, err := os.Stat(backupPath); err != nil {
		if err := copyFile(asarPath, backupPath); err != nil {
			fmt.Println("バックアップ作成に失敗しました:", err)
			pause()
			os.Exit(1)
		}
		fmt.Println("バックアップを作成しました:", backupPath)
	}

	tmpPath := asarPath + ".ja-tmp"
	hit := map[string]int{}
	n, err := repack(asarPath, tmpPath, dict, hit)
	if err != nil {
		fmt.Println("パッチ作成に失敗しました:", err)
		os.Remove(tmpPath)
		pause()
		os.Exit(1)
	}
	if n == 0 {
		fmt.Println("置換できる文字列が見つかりませんでした。中止します。")
		os.Remove(tmpPath)
		pause()
		os.Exit(1)
	}
	if err := os.Rename(tmpPath, asarPath); err != nil {
		// rename across same dir should work; fall back to copy
		if err2 := copyFile(tmpPath, asarPath); err2 != nil {
			fmt.Println("差し替えに失敗しました:", err)
			fmt.Println("Image MetaHubが起動中の場合は閉じてから再実行してください。")
			pause()
			os.Exit(1)
		}
		os.Remove(tmpPath)
	}
	fmt.Printf("完了しました（%d か所を日本語化）。\n", n)
	fmt.Println("Image MetaHub を起動して確認してください。")
	fmt.Println("元に戻すときは、このパッチャーをもう一度実行すると復元できます。")
	pause()
}

func copyFile(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()
	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return out.Close()
}

var stdin = bufio.NewReader(os.Stdin)

func readLine() string {
	s, _ := stdin.ReadString('\n')
	return s
}

func pause() {
	fmt.Print("Enterキーで終了します...")
	readLine()
}

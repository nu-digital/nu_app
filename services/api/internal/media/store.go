package media

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"regexp"
	"strings"
	"syscall"
	"time"
)

// Store は永続ボリューム（MEDIA_DIR）上の画像ファイル。全ファイルを同じ非公開ディレクトリに置き、
// Nginx等が直接配信しない。公開・非公開はAPIの判定で決める（ファイルの場所では区別しない）。
// 開く・書く・消すはすべて os.Root の下で行い、パストラバーサルをOSのAPIで防ぐ。
type Store struct {
	dir  string
	root *os.Root
}

var keyRe = regexp.MustCompile(`^[0-9a-f]{2}/[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\.(jpg|png)$`)

// ValidKey はstorage_keyの形式を検査する（DBの値でも、読み出し・削除の前に必ず確認する）。
func ValidKey(key string) bool { return keyRe.MatchString(key) }

// NewKey はUUID（小文字・ハイフン付き）と拡張子（jpg/png）からstorage_keyを作る。
func NewKey(id, ext string) (string, error) {
	k := id[:min(2, len(id))] + "/" + id + "." + ext
	if !ValidKey(k) {
		return "", errors.New("storage_keyの形式が正しくありません")
	}
	return k, nil
}

// OpenStore はディレクトリを開く（無ければ作る。0750）。tmpディレクトリも作る。
func OpenStore(dir string) (*Store, error) {
	if dir == "" {
		return nil, errors.New("MEDIA_DIRが空です")
	}
	if err := os.MkdirAll(path.Join(dir, "tmp"), 0o750); err != nil {
		return nil, fmt.Errorf("メディアの保存先を作れません")
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, fmt.Errorf("メディアの保存先を開けません")
	}
	return &Store{dir: dir, root: root}, nil
}

func (s *Store) Close() error { return s.root.Close() }

// Put は tmp に書いて fsync → 保存先へ rename → 親ディレクトリも fsync する（途中で落ちても中途半端なファイルが保存先に出ない）。
func (s *Store) Put(key string, data []byte) error {
	if !ValidKey(key) {
		return errors.New("storage_keyの形式が正しくありません")
	}
	var rnd [12]byte
	if _, err := rand.Read(rnd[:]); err != nil {
		return err
	}
	tmp := "tmp/" + hex.EncodeToString(rnd[:])
	f, err := s.root.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o640)
	if err != nil {
		return err
	}
	ok := false
	defer func() {
		if !ok {
			_ = f.Close()
			_ = s.root.Remove(tmp)
		}
	}()
	if _, err := f.Write(data); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	parent := path.Dir(key)
	if err := s.root.MkdirAll(parent, 0o750); err != nil {
		return err
	}
	if err := s.root.Rename(tmp, key); err != nil {
		return err
	}
	ok = true
	if d, err := s.root.Open(parent); err == nil { // 親ディレクトリのfsync（renameの永続化）
		_ = d.Sync()
		_ = d.Close()
	}
	return nil
}

// Open はファイルを開く。形式が不正なキーは開かない。
func (s *Store) Open(key string) (*os.File, error) {
	if !ValidKey(key) {
		return nil, fs.ErrNotExist
	}
	return s.root.Open(key)
}

// Remove はファイルを消す（無ければ無視）。
func (s *Store) Remove(key string) error {
	if !ValidKey(key) {
		return errors.New("storage_keyの形式が正しくありません")
	}
	if err := s.root.Remove(key); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

// FreeBytes は保存先の空き容量。
func (s *Store) FreeBytes() (uint64, error) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(s.dir, &st); err != nil {
		return 0, err
	}
	return st.Bavail * uint64(st.Bsize), nil
}

// Keys は保存されているファイルのstorage_keyを返す（tmpは除く。形式に合わないものは別に返す）。
func (s *Store) Keys() (keys, strays []string, err error) {
	err = fs.WalkDir(s.root.FS(), ".", func(p string, d fs.DirEntry, werr error) error {
		if werr != nil {
			return werr
		}
		if d.IsDir() || p == "." || strings.HasPrefix(p, "tmp/") {
			return nil
		}
		if ValidKey(p) && d.Type().IsRegular() {
			keys = append(keys, p)
		} else { // 形式に合わない名前、またはシンボリックリンク等（通常のファイルではない）
			strays = append(strays, p)
		}
		return nil
	})
	return
}

// CleanTmp は古いtmpファイル（取り込みの途中で落ちた残り）を消し、消した数を返す。
func (s *Store) CleanTmp(olderThan time.Duration) (int, error) {
	es, err := fs.ReadDir(s.root.FS(), "tmp")
	if err != nil {
		return 0, err
	}
	n := 0
	for _, e := range es {
		info, err := e.Info()
		if err != nil || e.IsDir() || time.Since(info.ModTime()) < olderThan {
			continue
		}
		if err := s.root.Remove("tmp/" + e.Name()); err == nil {
			n++
		}
	}
	return n, nil
}

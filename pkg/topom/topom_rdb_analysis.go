// Copyright 2016 CodisLabs. All Rights Reserved.
// Licensed under the MIT (MIT-LICENSE.txt) license.

package topom

import (
	stdcontext "context"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	rdbmodel "github.com/hdt3213/rdb/model"
	"github.com/hdt3213/rdb/parser"

	"github.com/CodisLabs/codis/pkg/utils/bytesize"
	"github.com/CodisLabs/codis/pkg/utils/errors"
	"github.com/CodisLabs/codis/pkg/utils/log"
)

const (
	RDBAnalysisStatusQueued   = "queued"
	RDBAnalysisStatusRunning  = "running"
	RDBAnalysisStatusDone     = "done"
	RDBAnalysisStatusError    = "error"
	RDBAnalysisStatusCanceled = "canceled"

	rdbAnalysisDefaultTopN        = 20
	rdbAnalysisDefaultMaxDepth    = 3
	rdbAnalysisMaxDepth           = 16
	rdbAnalysisSnapshotInterval   = 128
	rdbAnalysisMultipartMemoryMax = 32 << 20
)

type RDBAnalysisOptions struct {
	TopN             int      `json:"top_n"`
	PrefixSeparators []string `json:"prefix_separators"`
	MaxDepth         int      `json:"max_depth"`
	Regex            string   `json:"regex"`
	IncludeExpired   bool     `json:"include_expired"`
}

type RDBAnalysisSummary struct {
	Name         string `json:"name"`
	DB           int    `json:"db,omitempty"`
	Size         int64  `json:"size"`
	SizeReadable string `json:"size_readable"`
	KeyCount     int64  `json:"key_count"`
}

type RDBAnalysisKeyEntry struct {
	DB           int    `json:"db"`
	Key          string `json:"key"`
	Type         string `json:"type"`
	Size         int64  `json:"size"`
	SizeReadable string `json:"size_readable"`
	ElementCount int    `json:"element_count"`
	Encoding     string `json:"encoding,omitempty"`
	Frequency    int64  `json:"frequency,omitempty"`
}

type RDBAnalysisFlameNode struct {
	Name     string                  `json:"name"`
	Value    int64                   `json:"value"`
	Children []*RDBAnalysisFlameNode `json:"children,omitempty"`
}

type RDBAnalysisJob struct {
	ID          string             `json:"id"`
	CreatedAt   time.Time          `json:"created_at"`
	UpdatedAt   time.Time          `json:"updated_at"`
	Status      string             `json:"status"`
	Source      string             `json:"source"`
	Options     RDBAnalysisOptions `json:"options"`
	FileSize    int64              `json:"file_size"`
	BytesRead   int64              `json:"bytes_read"`
	ObjectsRead int64              `json:"objects_read"`
	DBCount     int                `json:"db_count"`
	TotalSize   int64              `json:"total_size"`

	TypeSummary   []RDBAnalysisSummary  `json:"type_summary"`
	DBSummary     []RDBAnalysisSummary  `json:"db_summary"`
	TopBigKeys    []RDBAnalysisKeyEntry `json:"top_big_keys"`
	TopHotKeys    []RDBAnalysisKeyEntry `json:"top_hot_keys"`
	PrefixSummary []RDBAnalysisSummary  `json:"prefix_summary"`
	FlameGraph    *RDBAnalysisFlameNode `json:"flamegraph,omitempty"`
	Error         string                `json:"error,omitempty"`

	mu      *sync.Mutex
	cancel  stdcontext.CancelFunc
	path    string
	cleanup bool
}

func (j *RDBAnalysisJob) Snapshot() *RDBAnalysisJob {
	j.mu.Lock()
	defer j.mu.Unlock()

	x := *j
	x.TypeSummary = append([]RDBAnalysisSummary(nil), j.TypeSummary...)
	x.DBSummary = append([]RDBAnalysisSummary(nil), j.DBSummary...)
	x.TopBigKeys = append([]RDBAnalysisKeyEntry(nil), j.TopBigKeys...)
	x.TopHotKeys = append([]RDBAnalysisKeyEntry(nil), j.TopHotKeys...)
	x.PrefixSummary = append([]RDBAnalysisSummary(nil), j.PrefixSummary...)
	x.FlameGraph = cloneRDBAnalysisFlameNode(j.FlameGraph)
	x.mu = nil
	x.cancel = nil
	x.path = ""
	x.cleanup = false
	return &x
}

func (j *RDBAnalysisJob) setStatus(status string, err error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.Status = status
	j.UpdatedAt = time.Now()
	if err != nil {
		j.Error = err.Error()
	}
}

func (j *RDBAnalysisJob) updateSnapshot(snapshot *rdbAnalysisSnapshot) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.UpdatedAt = time.Now()
	j.BytesRead = snapshot.bytesRead
	j.ObjectsRead = snapshot.objectsRead
	j.DBCount = len(snapshot.dbSet)
	j.TotalSize = snapshot.totalSize
	j.TypeSummary = snapshot.typeSummary()
	j.DBSummary = snapshot.dbSummary()
	j.TopBigKeys = append([]RDBAnalysisKeyEntry(nil), snapshot.topBigKeys...)
	j.TopHotKeys = append([]RDBAnalysisKeyEntry(nil), snapshot.topHotKeys...)
	j.PrefixSummary = snapshot.prefixSummary()
	j.FlameGraph = snapshot.flameRoot.export()
}

type RDBAnalysisManager struct {
	mu            sync.Mutex
	nextID        int64
	jobs          map[string]*RDBAnalysisJob
	workspace     string
	maxUpload     int64
	maxConcurrent int
	maxRetained   int
	maxTopN       int
}

func NewRDBAnalysisManager(config *Config) *RDBAnalysisManager {
	workspace := strings.TrimSpace(config.RDBAnalysisWorkspace)
	if workspace == "" {
		workspace = filepath.Join(os.TempDir(), "codis-rdb-analysis")
	}
	if abs, err := filepath.Abs(workspace); err == nil {
		workspace = abs
	}
	maxUpload := config.RDBAnalysisMaxUploadSize.Int64()
	if maxUpload <= 0 {
		maxUpload = bytesize.GB
	}
	maxConcurrent := config.RDBAnalysisMaxConcurrentJobs
	if maxConcurrent <= 0 {
		maxConcurrent = 1
	}
	maxRetained := config.RDBAnalysisMaxJobsRetained
	if maxRetained <= 0 {
		maxRetained = 16
	}
	maxTopN := config.RDBAnalysisMaxTopN
	if maxTopN <= 0 {
		maxTopN = 100
	}
	return &RDBAnalysisManager{
		jobs:          make(map[string]*RDBAnalysisJob),
		workspace:     workspace,
		maxUpload:     maxUpload,
		maxConcurrent: maxConcurrent,
		maxRetained:   maxRetained,
		maxTopN:       maxTopN,
	}
}

func (m *RDBAnalysisManager) MaxUploadSize() int64 {
	return m.maxUpload
}

func (m *RDBAnalysisManager) StartWorkspace(path string, options RDBAnalysisOptions) (*RDBAnalysisJob, error) {
	abs, rel, err := m.resolveWorkspacePath(path)
	if err != nil {
		return nil, err
	}
	st, err := os.Stat(abs)
	if err != nil {
		return nil, errors.Trace(err)
	}
	if st.IsDir() {
		return nil, errors.Errorf("rdb analysis path is a directory")
	}
	return m.startJob("workspace:"+rel, abs, st.Size(), false, options)
}

func (m *RDBAnalysisManager) StartUpload(filename string, reader io.Reader, options RDBAnalysisOptions) (*RDBAnalysisJob, error) {
	if err := os.MkdirAll(filepath.Join(m.workspace, "uploads"), 0755); err != nil {
		return nil, errors.Trace(err)
	}
	name := filepath.Base(filename)
	if name == "." || name == string(filepath.Separator) {
		name = "upload.rdb"
	}
	tmp, err := os.CreateTemp(filepath.Join(m.workspace, "uploads"), "rdb-analysis-*.rdb")
	if err != nil {
		return nil, errors.Trace(err)
	}
	path := tmp.Name()
	defer tmp.Close()

	limited := &io.LimitedReader{R: reader, N: m.maxUpload + 1}
	n, err := io.Copy(tmp, limited)
	if err != nil {
		os.Remove(path)
		return nil, errors.Trace(err)
	}
	if n > m.maxUpload {
		os.Remove(path)
		return nil, errors.Errorf("rdb upload exceeds max size %s", bytesize.Int64(m.maxUpload).HumanString())
	}
	return m.startJob("upload:"+name, path, n, true, options)
}

func (m *RDBAnalysisManager) Get(id string) (*RDBAnalysisJob, error) {
	m.mu.Lock()
	job := m.jobs[id]
	m.mu.Unlock()
	if job == nil {
		return nil, errors.Errorf("rdb analysis job %s not found", id)
	}
	return job.Snapshot(), nil
}

func (m *RDBAnalysisManager) Cancel(id string) error {
	m.mu.Lock()
	job := m.jobs[id]
	m.mu.Unlock()
	if job == nil {
		return errors.Errorf("rdb analysis job %s not found", id)
	}
	job.mu.Lock()
	cancel := job.cancel
	status := job.Status
	job.mu.Unlock()
	if status == RDBAnalysisStatusDone || status == RDBAnalysisStatusError || status == RDBAnalysisStatusCanceled {
		return nil
	}
	if cancel != nil {
		cancel()
	}
	return nil
}

func (m *RDBAnalysisManager) Remove(id string) error {
	m.mu.Lock()
	job := m.jobs[id]
	if job != nil {
		delete(m.jobs, id)
	}
	m.mu.Unlock()
	if job == nil {
		return errors.Errorf("rdb analysis job %s not found", id)
	}
	job.mu.Lock()
	cancel := job.cancel
	path := job.path
	cleanup := job.cleanup
	job.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if cleanup && path != "" {
		_ = os.Remove(path)
	}
	return nil
}

func (m *RDBAnalysisManager) Close() {
	m.mu.Lock()
	jobs := make([]*RDBAnalysisJob, 0, len(m.jobs))
	for _, job := range m.jobs {
		jobs = append(jobs, job)
	}
	m.mu.Unlock()
	for _, job := range jobs {
		job.mu.Lock()
		cancel := job.cancel
		job.mu.Unlock()
		if cancel != nil {
			cancel()
		}
	}
}

func (m *RDBAnalysisManager) startJob(source, path string, fileSize int64, cleanup bool, options RDBAnalysisOptions) (*RDBAnalysisJob, error) {
	options = m.normalizeOptions(options)
	if err := m.checkConcurrentLimit(); err != nil {
		if cleanup {
			_ = os.Remove(path)
		}
		return nil, err
	}

	ctx, cancel := stdcontext.WithCancel(stdcontext.Background())
	now := time.Now()
	m.mu.Lock()
	m.nextID++
	id := strconv.FormatInt(m.nextID, 10)
	job := &RDBAnalysisJob{
		ID:        id,
		CreatedAt: now,
		UpdatedAt: now,
		Status:    RDBAnalysisStatusQueued,
		Source:    source,
		Options:   options,
		FileSize:  fileSize,
		mu:        &sync.Mutex{},
		cancel:    cancel,
		path:      path,
		cleanup:   cleanup,
	}
	m.jobs[id] = job
	m.pruneLocked()
	m.mu.Unlock()

	go m.runJob(ctx, job)
	return job.Snapshot(), nil
}

func (m *RDBAnalysisManager) checkConcurrentLimit() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	var n int
	for _, job := range m.jobs {
		job.mu.Lock()
		status := job.Status
		job.mu.Unlock()
		if status == RDBAnalysisStatusQueued || status == RDBAnalysisStatusRunning {
			n++
		}
	}
	if n >= m.maxConcurrent {
		return errors.Errorf("too many running rdb analysis jobs")
	}
	return nil
}

func (m *RDBAnalysisManager) pruneLocked() {
	if len(m.jobs) <= m.maxRetained {
		return
	}
	jobs := make([]*RDBAnalysisJob, 0, len(m.jobs))
	for _, job := range m.jobs {
		job.mu.Lock()
		status := job.Status
		job.mu.Unlock()
		if status != RDBAnalysisStatusQueued && status != RDBAnalysisStatusRunning {
			jobs = append(jobs, job)
		}
	}
	sort.SliceStable(jobs, func(i, j int) bool {
		return jobs[i].CreatedAt.Before(jobs[j].CreatedAt)
	})
	for len(m.jobs) > m.maxRetained && len(jobs) > 0 {
		job := jobs[0]
		jobs = jobs[1:]
		delete(m.jobs, job.ID)
		job.mu.Lock()
		path := job.path
		cleanup := job.cleanup
		job.mu.Unlock()
		if cleanup && path != "" {
			_ = os.Remove(path)
		}
	}
}

func (m *RDBAnalysisManager) normalizeOptions(options RDBAnalysisOptions) RDBAnalysisOptions {
	if options.TopN <= 0 {
		options.TopN = rdbAnalysisDefaultTopN
	}
	if options.TopN > m.maxTopN {
		options.TopN = m.maxTopN
	}
	options.PrefixSeparators = normalizeRDBAnalysisSeparators(options.PrefixSeparators)
	if options.MaxDepth < 0 {
		options.MaxDepth = 0
	} else {
		options.MaxDepth = maxRDBAnalysisDepth(options.MaxDepth)
	}
	return options
}

func (m *RDBAnalysisManager) resolveWorkspacePath(path string) (string, string, error) {
	if strings.TrimSpace(path) == "" {
		return "", "", errors.New("missing rdb analysis path")
	}
	workspace, err := filepath.Abs(m.workspace)
	if err != nil {
		return "", "", errors.Trace(err)
	}
	if resolved, err := filepath.EvalSymlinks(workspace); err == nil {
		workspace = resolved
	}
	clean := filepath.Clean(path)
	if !filepath.IsAbs(clean) {
		clean = filepath.Join(workspace, clean)
	}
	abs, err := filepath.Abs(clean)
	if err != nil {
		return "", "", errors.Trace(err)
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		abs = resolved
	}
	rel, err := filepath.Rel(workspace, abs)
	if err != nil {
		return "", "", errors.Trace(err)
	}
	if rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", "", errors.Errorf("rdb analysis path is outside workspace")
	}
	return abs, rel, nil
}

func (m *RDBAnalysisManager) runJob(ctx stdcontext.Context, job *RDBAnalysisJob) {
	job.setStatus(RDBAnalysisStatusRunning, nil)
	start := time.Now()
	log.Warnf("rdb analysis job-[%s] start source=%s size=%d", job.ID, job.Source, job.FileSize)

	err := m.parseJob(ctx, job)
	switch {
	case ctx.Err() != nil:
		job.setStatus(RDBAnalysisStatusCanceled, nil)
		log.Warnf("rdb analysis job-[%s] canceled in %v", job.ID, time.Since(start))
	case err != nil:
		job.setStatus(RDBAnalysisStatusError, err)
		log.WarnErrorf(err, "rdb analysis job-[%s] failed in %v", job.ID, time.Since(start))
	default:
		job.setStatus(RDBAnalysisStatusDone, nil)
		log.Warnf("rdb analysis job-[%s] done in %v", job.ID, time.Since(start))
	}
}

func (m *RDBAnalysisManager) parseJob(ctx stdcontext.Context, job *RDBAnalysisJob) error {
	file, err := os.Open(job.path)
	if err != nil {
		return errors.Trace(err)
	}
	defer file.Close()

	decoder := parser.NewDecoder(file)
	snapshot, err := newRDBAnalysisSnapshot(job.Options)
	if err != nil {
		return err
	}
	now := time.Now()
	err = decoder.Parse(func(object rdbmodel.RedisObject) bool {
		if ctx.Err() != nil {
			return false
		}
		if snapshot.accept(object, now) {
			snapshot.add(object, int64(decoder.GetReadCount()))
			if snapshot.objectsRead%rdbAnalysisSnapshotInterval == 0 {
				job.updateSnapshot(snapshot)
			}
		} else {
			snapshot.bytesRead = int64(decoder.GetReadCount())
		}
		return true
	})
	snapshot.bytesRead = int64(decoder.GetReadCount())
	job.updateSnapshot(snapshot)
	if ctx.Err() != nil {
		return nil
	}
	if err != nil {
		return errors.Trace(err)
	}
	return nil
}

type rdbAnalysisSnapshot struct {
	options     RDBAnalysisOptions
	regex       *regexp.Regexp
	bytesRead   int64
	objectsRead int64
	totalSize   int64
	dbSet       map[int]bool
	typeStats   map[string]*RDBAnalysisSummary
	dbStats     map[int]*RDBAnalysisSummary
	prefixStats map[string]*RDBAnalysisSummary
	topBigKeys  []RDBAnalysisKeyEntry
	topHotKeys  []RDBAnalysisKeyEntry
	flameRoot   *rdbAnalysisFlameBuilder
}

func newRDBAnalysisSnapshot(options RDBAnalysisOptions) (*rdbAnalysisSnapshot, error) {
	var reg *regexp.Regexp
	var err error
	if options.Regex != "" {
		reg, err = regexp.Compile(options.Regex)
		if err != nil {
			return nil, errors.Errorf("illegal regex expression: %s", options.Regex)
		}
	}
	return &rdbAnalysisSnapshot{
		options:     options,
		regex:       reg,
		dbSet:       make(map[int]bool),
		typeStats:   make(map[string]*RDBAnalysisSummary),
		dbStats:     make(map[int]*RDBAnalysisSummary),
		prefixStats: make(map[string]*RDBAnalysisSummary),
		flameRoot:   newRDBAnalysisFlameBuilder("root"),
	}, nil
}

func (s *rdbAnalysisSnapshot) accept(object rdbmodel.RedisObject, now time.Time) bool {
	if s.regex != nil && !s.regex.MatchString(object.GetKey()) {
		return false
	}
	if !s.options.IncludeExpired {
		if expiration := object.GetExpiration(); expiration != nil && !expiration.After(now) {
			return false
		}
	}
	return true
}

func (s *rdbAnalysisSnapshot) add(object rdbmodel.RedisObject, bytesRead int64) {
	size := int64(object.GetSize())
	db := object.GetDBIndex()
	typ := object.GetType()
	s.bytesRead = bytesRead
	s.objectsRead++
	s.totalSize += size
	s.dbSet[db] = true

	typeStat := s.typeStats[typ]
	if typeStat == nil {
		typeStat = &RDBAnalysisSummary{Name: typ}
		s.typeStats[typ] = typeStat
	}
	typeStat.Size += size
	typeStat.KeyCount++
	typeStat.SizeReadable = bytesize.Int64(typeStat.Size).HumanString()

	dbStat := s.dbStats[db]
	if dbStat == nil {
		dbStat = &RDBAnalysisSummary{Name: fmt.Sprintf("db%d", db), DB: db}
		s.dbStats[db] = dbStat
	}
	dbStat.Size += size
	dbStat.KeyCount++
	dbStat.SizeReadable = bytesize.Int64(dbStat.Size).HumanString()

	entry := newRDBAnalysisKeyEntry(object, 0)
	s.topBigKeys = addTopKeyBySize(s.topBigKeys, entry, s.options.TopN)
	if evict, ok := object.(rdbmodel.EvictionInfo); ok {
		if freq := evict.GetFreq(); freq >= 0 {
			entry.Frequency = freq
			s.topHotKeys = addTopKeyByFrequency(s.topHotKeys, entry, s.options.TopN)
		}
	}

	for _, prefix := range analysisPrefixes(db, object.GetKey(), s.options.PrefixSeparators, s.options.MaxDepth) {
		stat := s.prefixStats[prefix]
		if stat == nil {
			stat = &RDBAnalysisSummary{Name: prefix, DB: db}
			s.prefixStats[prefix] = stat
		}
		stat.Size += size
		stat.KeyCount++
		stat.SizeReadable = bytesize.Int64(stat.Size).HumanString()
	}
	s.flameRoot.add(db, object.GetKey(), size, s.options.PrefixSeparators, s.options.MaxDepth)
}

func (s *rdbAnalysisSnapshot) typeSummary() []RDBAnalysisSummary {
	list := make([]RDBAnalysisSummary, 0, len(s.typeStats))
	for _, stat := range s.typeStats {
		list = append(list, *stat)
	}
	sort.SliceStable(list, func(i, j int) bool {
		if list[i].Size == list[j].Size {
			return list[i].Name < list[j].Name
		}
		return list[i].Size > list[j].Size
	})
	return list
}

func (s *rdbAnalysisSnapshot) dbSummary() []RDBAnalysisSummary {
	list := make([]RDBAnalysisSummary, 0, len(s.dbStats))
	for _, stat := range s.dbStats {
		list = append(list, *stat)
	}
	sort.SliceStable(list, func(i, j int) bool {
		return list[i].DB < list[j].DB
	})
	return list
}

func (s *rdbAnalysisSnapshot) prefixSummary() []RDBAnalysisSummary {
	list := make([]RDBAnalysisSummary, 0, len(s.prefixStats))
	for _, stat := range s.prefixStats {
		list = append(list, *stat)
	}
	sort.SliceStable(list, func(i, j int) bool {
		if list[i].Size == list[j].Size {
			return list[i].Name < list[j].Name
		}
		return list[i].Size > list[j].Size
	})
	if len(list) > s.options.TopN {
		list = list[:s.options.TopN]
	}
	return list
}

func newRDBAnalysisKeyEntry(object rdbmodel.RedisObject, frequency int64) RDBAnalysisKeyEntry {
	size := int64(object.GetSize())
	return RDBAnalysisKeyEntry{
		DB:           object.GetDBIndex(),
		Key:          object.GetKey(),
		Type:         object.GetType(),
		Size:         size,
		SizeReadable: bytesize.Int64(size).HumanString(),
		ElementCount: object.GetElemCount(),
		Encoding:     object.GetEncoding(),
		Frequency:    frequency,
	}
}

func addTopKeyBySize(list []RDBAnalysisKeyEntry, entry RDBAnalysisKeyEntry, limit int) []RDBAnalysisKeyEntry {
	list = append(list, entry)
	sort.SliceStable(list, func(i, j int) bool {
		if list[i].Size == list[j].Size {
			return list[i].Key < list[j].Key
		}
		return list[i].Size > list[j].Size
	})
	if len(list) > limit {
		list = list[:limit]
	}
	return list
}

func addTopKeyByFrequency(list []RDBAnalysisKeyEntry, entry RDBAnalysisKeyEntry, limit int) []RDBAnalysisKeyEntry {
	list = append(list, entry)
	sort.SliceStable(list, func(i, j int) bool {
		if list[i].Frequency == list[j].Frequency {
			return list[i].Key < list[j].Key
		}
		return list[i].Frequency > list[j].Frequency
	})
	if len(list) > limit {
		list = list[:limit]
	}
	return list
}

func analysisPrefixes(db int, key string, separators []string, maxDepth int) []string {
	parts := splitAnalysisKey(key, separators)
	if len(parts) <= 1 {
		return nil
	}
	limit := len(parts) - 1
	maxDepth = maxRDBAnalysisDepth(maxDepth)
	if limit > maxDepth {
		limit = maxDepth
	}
	prefixes := make([]string, 0, limit)
	sep := primaryRDBAnalysisSeparator(separators)
	for depth := 1; depth <= limit; depth++ {
		prefixes = append(prefixes, fmt.Sprintf("db%d:%s%s*", db, strings.Join(parts[:depth], sep), sep))
	}
	return prefixes
}

func splitAnalysisKey(key string, separators []string) []string {
	sep := primaryRDBAnalysisSeparator(separators)
	normalized := key
	for i := 1; i < len(separators); i++ {
		if separators[i] != "" {
			normalized = strings.ReplaceAll(normalized, separators[i], sep)
		}
	}
	return strings.Split(normalized, sep)
}

type rdbAnalysisFlameBuilder struct {
	name     string
	value    int64
	children map[string]*rdbAnalysisFlameBuilder
}

func newRDBAnalysisFlameBuilder(name string) *rdbAnalysisFlameBuilder {
	return &rdbAnalysisFlameBuilder{name: name, children: make(map[string]*rdbAnalysisFlameBuilder)}
}

func (n *rdbAnalysisFlameBuilder) add(db int, key string, size int64, separators []string, maxDepth int) {
	parts := append([]string{fmt.Sprintf("db%d", db)}, splitAnalysisKey(key, separators)...)
	maxDepth = maxRDBAnalysisDepth(maxDepth)
	limit := maxDepth + 1
	if limit > len(parts) {
		limit = len(parts)
	}
	node := n
	node.value += size
	for _, part := range parts[:limit] {
		child := node.children[part]
		if child == nil {
			child = newRDBAnalysisFlameBuilder(part)
			node.children[part] = child
		}
		child.value += size
		node = child
	}
}

func (n *rdbAnalysisFlameBuilder) export() *RDBAnalysisFlameNode {
	if n == nil {
		return nil
	}
	out := &RDBAnalysisFlameNode{Name: n.name, Value: n.value}
	names := make([]string, 0, len(n.children))
	for name := range n.children {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		out.Children = append(out.Children, n.children[name].export())
	}
	return out
}

func cloneRDBAnalysisFlameNode(n *RDBAnalysisFlameNode) *RDBAnalysisFlameNode {
	if n == nil {
		return nil
	}
	x := &RDBAnalysisFlameNode{Name: n.Name, Value: n.Value}
	for _, child := range n.Children {
		x.Children = append(x.Children, cloneRDBAnalysisFlameNode(child))
	}
	return x
}

func parseRDBAnalysisBool(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "1", "true", "yes", "y", "on":
		return true
	default:
		return false
	}
}

func parseRDBAnalysisInt(s string) int {
	text := strings.TrimSpace(s)
	if text == "" {
		return 0
	}
	n, err := strconv.Atoi(text)
	if err != nil {
		log.WarnErrorf(err, "parse rdb analysis integer %q failed", text)
		return 0
	}
	return n
}

func splitRDBAnalysisSeparators(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

func normalizeRDBAnalysisSeparators(separators []string) []string {
	out := make([]string, 0, len(separators))
	for _, sep := range separators {
		if sep = strings.TrimSpace(sep); sep != "" {
			out = append(out, sep)
		}
	}
	if len(out) == 0 {
		return []string{":"}
	}
	return out
}

func primaryRDBAnalysisSeparator(separators []string) string {
	if len(separators) == 0 || separators[0] == "" {
		return ":"
	}
	return separators[0]
}

func maxRDBAnalysisDepth(maxDepth int) int {
	if maxDepth <= 0 {
		return rdbAnalysisDefaultMaxDepth
	}
	return int(math.Min(float64(maxDepth), rdbAnalysisMaxDepth))
}

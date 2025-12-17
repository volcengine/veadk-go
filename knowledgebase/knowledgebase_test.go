package knowledgebase

import (
	"errors"
	"testing"

	"github.com/bytedance/mockey"
	"github.com/stretchr/testify/assert"
	"github.com/volcengine/veadk-go/knowledgebase/backend/viking_knowledge_backend"
	_interface "github.com/volcengine/veadk-go/knowledgebase/interface"
	"github.com/volcengine/veadk-go/knowledgebase/ktypes"
)

type mockBackend struct{}

func (m *mockBackend) Index() string { return "mock-index" }
func (m *mockBackend) Search(query string, opts ...map[string]any) ([]ktypes.KnowledgeEntry, error) {
	return []ktypes.KnowledgeEntry{{Content: "c", Metadata: map[string]any{"q": query}}}, nil
}
func (m *mockBackend) AddFromText(text []string, opts ...map[string]any) error         { return nil }
func (m *mockBackend) AddFromFiles(files []string, opts ...map[string]any) error       { return nil }
func (m *mockBackend) AddFromDirectory(directory string, opts ...map[string]any) error { return nil }

func TestNewKnowledgeBase_WithBackendInterface(t *testing.T) {
	var mock _interface.KnowledgeBackend = &mockBackend{}
	kb, err := NewKnowledgeBase(mock, WithName("n"), WithDescription("d"))
	assert.Nil(t, err)
	assert.NotNil(t, kb)
	assert.Equal(t, "n", kb.Name)
	assert.Equal(t, "d", kb.Description)
	assert.Equal(t, mock, kb.Backend)
}

func TestNewKnowledgeBase_Defaults(t *testing.T) {
	var mock _interface.KnowledgeBackend = &mockBackend{}
	kb, err := NewKnowledgeBase(mock)
	assert.Nil(t, err)
	assert.NotNil(t, kb)
	assert.Equal(t, DefaultName, kb.Name)
	assert.Equal(t, DefaultDescription, kb.Description)
}

func TestNewKnowledgeBase_WithStringBackendAndValidConfig(t *testing.T) {
	mockey.PatchConvey("viking backend with valid config via mock constructor", t, func() {
		var m _interface.KnowledgeBackend = &mockBackend{}
		mockey.Mock(viking_knowledge_backend.NewVikingKnowledgeBackend).Return(m, nil).Build()

		kb, err := NewKnowledgeBase(
			ktypes.VikingBackend,
			WithBackendConfig(&viking_knowledge_backend.Config{Index: "idx"}),
			WithName("n"),
			WithDescription("d"),
		)
		assert.Nil(t, err)
		assert.NotNil(t, kb)
		assert.Equal(t, "n", kb.Name)
		assert.Equal(t, "d", kb.Description)
		assert.Equal(t, m, kb.Backend)
	})
}

func TestNewKnowledgeBase_VikingConstructorError(t *testing.T) {
	mockey.PatchConvey("viking backend constructor returns error", t, func() {
		mockey.Mock(viking_knowledge_backend.NewVikingKnowledgeBackend).Return(nil, errors.New("ctor error")).Build()

		kb, err := NewKnowledgeBase(
			ktypes.VikingBackend,
			WithBackendConfig(&viking_knowledge_backend.Config{Index: "idx"}),
		)
		assert.Nil(t, kb)
		assert.NotNil(t, err)
	})
}

func TestNewKnowledgeBase_InvalidConfigType(t *testing.T) {
	mockey.PatchConvey("viking backend with invalid config type", t, func() {
		kb, err := NewKnowledgeBase(
			ktypes.VikingBackend,
			WithBackendConfig(struct{}{}),
		)
		assert.Nil(t, kb)
		assert.True(t, errors.Is(err, ErrInvalidKnowledgeBackendConfig))
	})
}

func TestGetKnowledgeBackend_Unsupported(t *testing.T) {
	mockey.PatchConvey("unsupported backend types return wrapped error", t, func() {
		for _, b := range []string{ktypes.RedisBackend, ktypes.LocalBackend, ktypes.OpensearchBackend, "unknown"} {
			kb, err := getKnowledgeBackend(b, nil)
			assert.Nil(t, kb)
			assert.True(t, errors.Is(err, ErrInvalidKnowledgeBackend))
		}
	})
}

func TestNewKnowledgeBase_InvalidBackendType(t *testing.T) {
	kb, err := NewKnowledgeBase(123)
	assert.Nil(t, kb)
	assert.True(t, errors.Is(err, ErrInvalidKnowledgeBackend))
}

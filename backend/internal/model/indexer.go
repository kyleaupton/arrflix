package model

import (
	"time"
)

// IndexerSchema represents the complete indexer schema response from Prowlarr
type IndexerSchema []IndexerDefinition

// IndexerDefinition represents a single indexer definition
type IndexerDefinition struct {
	Added              time.Time           `json:"added" swagger:"description:When the indexer was added"`
	AppProfileID       int                 `json:"appProfileId" swagger:"description:Application profile ID"`
	Capabilities       IndexerCapabilities `json:"capabilities" swagger:"description:Indexer capabilities"`
	ConfigContract     string              `json:"configContract" swagger:"description:Configuration contract name"`
	DefinitionName     string              `json:"definitionName" swagger:"description:Definition name"`
	Description        string              `json:"description" swagger:"description:Indexer description"`
	DownloadClientID   int                 `json:"downloadClientId" swagger:"description:Download client ID"`
	Enable             bool                `json:"enable" swagger:"description:Whether the indexer is enabled"`
	Encoding           string              `json:"encoding" swagger:"description:Text encoding"`
	Fields             []IndexerField      `json:"fields" swagger:"description:Configuration fields"`
	Implementation     string              `json:"implementation" swagger:"description:Implementation name"`
	ImplementationName string              `json:"implementationName" swagger:"description:Implementation display name"`
	IndexerUrls        []string            `json:"indexerUrls" swagger:"description:Available indexer URLs"`
	InfoLink           string              `json:"infoLink" swagger:"description:Information link"`
	Language           string              `json:"language" swagger:"description:Language code"`
	LegacyUrls         []string            `json:"legacyUrls" swagger:"description:Legacy URLs"`
	Name               string              `json:"name" swagger:"description:Indexer name"`
	Presets            []any               `json:"presets" swagger:"description:Configuration presets"`
	Priority           int                 `json:"priority" swagger:"description:Indexer priority"`
	Privacy            string              `json:"privacy" swagger:"description:Privacy level (public, private, semiPrivate)"`
	Protocol           string              `json:"protocol" swagger:"description:Protocol (torrent, usenet)"`
	Redirect           bool                `json:"redirect" swagger:"description:Whether redirects are supported"`
	SortName           string              `json:"sortName" swagger:"description:Name used for sorting"`
	SupportsPagination bool                `json:"supportsPagination" swagger:"description:Whether pagination is supported"`
	SupportsRedirect   bool                `json:"supportsRedirect" swagger:"description:Whether redirects are supported"`
	SupportsRss        bool                `json:"supportsRss" swagger:"description:Whether RSS is supported"`
	SupportsSearch     bool                `json:"supportsSearch" swagger:"description:Whether search is supported"`
	Tags               []string            `json:"tags" swagger:"description:Indexer tags"`
}

// IndexerCapabilities represents the capabilities of an indexer
type IndexerCapabilities struct {
	BookSearchParams  []string          `json:"bookSearchParams" swagger:"description:Book search parameters"`
	Categories        []IndexerCategory `json:"categories" swagger:"description:Supported categories"`
	LimitsDefault     int               `json:"limitsDefault" swagger:"description:Default query limits"`
	LimitsMax         int               `json:"limitsMax" swagger:"description:Maximum query limits"`
	MovieSearchParams []string          `json:"movieSearchParams" swagger:"description:Movie search parameters"`
	MusicSearchParams []string          `json:"musicSearchParams" swagger:"description:Music search parameters"`
	SearchParams      []string          `json:"searchParams" swagger:"description:General search parameters"`
	SupportsRawSearch bool              `json:"supportsRawSearch" swagger:"description:Whether raw search is supported"`
	TVSearchParams    []string          `json:"tvSearchParams" swagger:"description:TV search parameters"`
}

// IndexerCategory represents a category supported by an indexer
type IndexerCategory struct {
	ID            int               `json:"id" swagger:"description:Category ID"`
	Name          string            `json:"name" swagger:"description:Category name"`
	SubCategories []IndexerCategory `json:"subCategories" swagger:"description:Sub-categories"`
}

// IndexerField represents a configuration field for an indexer
type IndexerField struct {
	Advanced                    bool                  `json:"advanced" swagger:"description:Whether this is an advanced field"`
	HelpLink                    string                `json:"helpLink,omitempty" swagger:"description:Help link URL"`
	HelpText                    string                `json:"helpText,omitempty" swagger:"description:Help text"`
	HelpTextWarning             string                `json:"helpTextWarning,omitempty" swagger:"description:Help text warning"`
	Hidden                      string                `json:"hidden,omitempty" swagger:"description:Hidden field type"`
	IsFloat                     bool                  `json:"isFloat" swagger:"description:Whether the value is a float"`
	Label                       string                `json:"label" swagger:"description:Field label"`
	Name                        string                `json:"name" swagger:"description:Field name"`
	Order                       int                   `json:"order" swagger:"description:Field order"`
	Privacy                     string                `json:"privacy" swagger:"description:Privacy level"`
	SelectOptions               []IndexerSelectOption `json:"selectOptions,omitempty" swagger:"description:Select options"`
	SelectOptionsProviderAction string                `json:"selectOptionsProviderAction,omitempty" swagger:"description:Select options provider action"`
	Type                        string                `json:"type" swagger:"description:Field type"`
	Unit                        string                `json:"unit,omitempty" swagger:"description:Value unit"`
	Value                       any                   `json:"value,omitempty" swagger:"description:Default value"`
}

// IndexerSelectOption represents an option for a select field
type IndexerSelectOption struct {
	Hint  string `json:"hint,omitempty" swagger:"description:Option hint"`
	Name  string `json:"name" swagger:"description:Option name"`
	Order int    `json:"order" swagger:"description:Option order"`
	Value any    `json:"value" swagger:"description:Option value"`
}

// Protocol used to download media. Comes with enum constants.
type Protocol string

// These are all the starr-supported protocols.
const (
	ProtocolUnknown Protocol = "unknown"
	ProtocolUsenet  Protocol = "usenet"
	ProtocolTorrent Protocol = "torrent"
)

// IndexerOutput is the output from the indexer methods.
type IndexerOutput struct {
	Enable             bool           `json:"enable"`
	Redirect           bool           `json:"redirect"`
	SupportsRss        bool           `json:"supportsRss"`
	SupportsSearch     bool           `json:"supportsSearch"`
	SupportsRedirect   bool           `json:"supportsRedirect"`
	AppProfileID       int64          `json:"appProfileId"`
	ID                 int64          `json:"id,omitempty"`
	Priority           int64          `json:"priority"`
	SortName           string         `json:"sortName"`
	Name               string         `json:"name"`
	Protocol           Protocol       `json:"protocol"`
	Privacy            string         `json:"privacy"`
	DefinitionName     string         `json:"definitionName"`
	Description        string         `json:"description"`
	Language           string         `json:"language"`
	Encoding           string         `json:"encoding,omitempty"`
	ImplementationName string         `json:"implementationName"`
	Implementation     string         `json:"implementation"`
	ConfigContract     string         `json:"configContract"`
	InfoLink           string         `json:"infoLink"`
	Added              time.Time      `json:"added"`
	Capabilities       *Capabilities  `json:"capabilities,omitempty"`
	Tags               []int          `json:"tags"`
	IndexerUrls        []string       `json:"indexerUrls"`
	LegacyUrls         []string       `json:"legacyUrls"`
	Fields             []*FieldOutput `json:"fields"`
}

// Capabilities is part of IndexerOutput.
type Capabilities struct {
	SupportsRawSearch bool          `json:"supportsRawSearch"`
	LimitsMax         int64         `json:"limitsMax"`
	LimitsDefault     int64         `json:"limitsDefault"`
	SearchParams      []string      `json:"searchParams"`
	TvSearchParams    []string      `json:"tvSearchParams"`
	MovieSearchParams []string      `json:"movieSearchParams"`
	MusicSearchParams []string      `json:"musicSearchParams"`
	BookSearchParams  []string      `json:"bookSearchParams"`
	Categories        []*Categories `json:"categories"`
}

// Categories is part of Capabilities.
type Categories struct {
	ID            int64         `json:"id"`
	Name          string        `json:"name"`
	SubCategories []*Categories `json:"subCategories"`
}

// FieldOutput is generic Name/Value struct applied to a few places.
type FieldOutput struct {
	Advanced                    bool            `json:"advanced,omitempty"`
	Order                       int64           `json:"order,omitempty"`
	HelpLink                    string          `json:"helpLink,omitempty"`
	HelpText                    string          `json:"helpText,omitempty"`
	Hidden                      string          `json:"hidden,omitempty"`
	Label                       string          `json:"label,omitempty"`
	Name                        string          `json:"name"`
	SelectOptionsProviderAction string          `json:"selectOptionsProviderAction,omitempty"`
	Type                        string          `json:"type,omitempty"`
	Privacy                     string          `json:"privacy"`
	Value                       any             `json:"value,omitempty"`
	SelectOptions               []*SelectOption `json:"selectOptions,omitempty"`
}

// SelectOption is part of Field.
type SelectOption struct {
	DividerAfter bool   `json:"dividerAfter,omitempty"`
	Order        int64  `json:"order"`
	Value        int64  `json:"value"`
	Hint         string `json:"hint"`
	Name         string `json:"name"`
}

// FieldInput is generic Name/Value struct applied to a few places.
type FieldInput struct {
	Name  string `json:"name"`
	Value any    `json:"value,omitempty"`
}

type IndexerInput struct {
	Enable         bool          `json:"enable"`
	Redirect       bool          `json:"redirect"`
	Priority       int64         `json:"priority"`
	ID             int64         `json:"id,omitempty"`
	AppProfileID   int64         `json:"appProfileId"`
	ConfigContract string        `json:"configContract"`
	Implementation string        `json:"implementation"`
	Name           string        `json:"name"`
	Protocol       Protocol      `json:"protocol"`
	Tags           []int         `json:"tags,omitempty"`
	Fields         []*FieldInput `json:"fields"`
}

// IndexerTestResult represents the result of testing a single indexer
type IndexerTestResult struct {
	Success bool   `json:"success" swagger:"description:Whether the test passed"`
	Message string `json:"message,omitempty" swagger:"description:Success message"`
	Error   string `json:"error,omitempty" swagger:"description:Error message if test failed"`
}

// IndexerBatchTestResult represents test results for batch testing
type IndexerBatchTestResult struct {
	IndexerID   int64  `json:"indexerId" swagger:"description:Indexer ID"`
	IndexerName string `json:"indexerName" swagger:"description:Indexer name"`
	Success     bool   `json:"success" swagger:"description:Whether the test passed"`
	Message     string `json:"message,omitempty" swagger:"description:Success message"`
	Error       string `json:"error,omitempty" swagger:"description:Error message if test failed"`
}

package graphql

import (
	"sync"

	"github.com/riducms/ridu"
	"github.com/riducms/ridu/schema"
)

const (
	adminPackage = "@riducms/plugin-graphql"
	// AdminPluginPairingVersion changes when the Go plugin and its admin package
	// stop agreeing on the playground loader or route.
	AdminPluginPairingVersion = 1
	playgroundRoute           = "graphql"
	playgroundLoader          = "graphql-playground"
)

type playgroundData struct {
	// Endpoint is the same-origin path of the GraphQL transport.
	Endpoint string `json:"endpoint"`
	// Schema is the transport's SDL, as written by ridu generate.
	Schema string `json:"schema"`
}

// Each config transform creates one loader and therefore one cache. Keeping this state out of the
// plugin value prevents applications that reuse the same configured plugin from sharing schemas.
type playgroundLoaderState struct {
	once sync.Once
	data playgroundData
	err  error
}

func (state *playgroundLoaderState) load(manifest schema.Manifest, options Options) (playgroundData, error) {
	state.once.Do(func() {
		state.data.Endpoint = options.Path
		state.data.Schema, state.err = GenerateSDL(manifest, options)
	})
	return state.data, state.err
}

// TransformConfig registers the playground's admin loader. The loader follows
// the admin's own access policy instead of enabling GraphQL introspection.
func (plugin *plugin) TransformConfig(config ridu.Config) (ridu.Config, error) {
	if plugin.options.DisablePlayground {
		return config, nil
	}
	state := &playgroundLoaderState{}
	options := plugin.options
	config.Admin.Loaders = append(config.Admin.Loaders, ridu.NewAdminLoader(playgroundLoader,
		func(ctx ridu.AdminLoadContext, _ struct{}) (playgroundData, error) {
			return state.load(ctx.Manifest(), options)
		},
	))
	return config, nil
}

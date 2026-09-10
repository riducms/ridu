package color_test

import (
	"testing"

	"github.com/riducms/ridu"
	color "github.com/riducms/ridu/examples/documentation/custom-components/color"
	"github.com/riducms/ridu/field"
	"github.com/riducms/ridu/plugintest"
	"github.com/riducms/ridu/store"
)

func TestConformance(t *testing.T) {
	plugintest.Run(t, plugintest.Fixture{
		Plugin: color.New(),
		Fields: field.Fields{
			color.Field("accent").Required(),
		},
		ValidData:   store.Values{"accent": store.String("#663399")},
		InvalidData: store.Values{"accent": store.String("purple")},
		Compatibility: []plugintest.CompatibilityCase{
			{RiduVersion: ridu.FrameworkVersion, Compatible: true},
		},
	})
}

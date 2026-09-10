package generate

import (
	"strings"
	"testing"

	"github.com/riducms/ridu/core"
	"github.com/riducms/ridu/field"
)

func TestGeneratedGoSelectNames(t *testing.T) {
	for _, test := range []struct {
		name   string
		fields field.Fields
		bad    bool
	}{
		{"without blocks", field.Fields{field.Select("tone", "light", "dark")}, false},
		{"normalized options collide", field.Fields{field.Select("tone", "light-mode", "light_mode")}, true},
		{"constant collides with type", field.Fields{field.Select("tone", "light"), field.Select("toneLight", "dark")}, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			manifest, err := core.Resolve(core.Config{Name: "Choices", Collections: []core.Collection{{Slug: "pages", Fields: test.fields}}})
			if err != nil {
				t.Fatal(err)
			}
			data, err := goClient(manifest)
			if test.bad {
				if err == nil || !strings.Contains(err.Error(), "generated Go symbol collides") {
					t.Fatalf("expected actionable symbol collision, got %v", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			for _, fragment := range []string{"type PageTone string", `const PageToneLight PageTone = "light"`, "*core.Input[PageTone]", "*PageTone"} {
				if !strings.Contains(string(data), fragment) {
					t.Errorf("missing %q", fragment)
				}
			}
		})
	}
}

func TestGeneratedGoArrayRowKeyCollisions(t *testing.T) {
	hero := field.Block{TypeName: "Hero", Slug: "hero", Fields: field.Fields{field.Text("rowKey"), field.Array("links", field.Fields{field.Group("rowKey", field.Fields{field.Select("tone", "dark", "light")}), field.Text("rowKeyField")}), field.Array("plain", field.Fields{field.Text("rowKey")})}}

	manifest, err := core.Resolve(core.Config{
		Name:         "Row identity collisions",
		Localization: core.LocalizationConfig{DefaultLocale: "en", Locales: []core.Locale{{Code: "en", Label: "English"}}},
		Collections:  []core.Collection{{Slug: "pages", Fields: field.Fields{field.Blocks("layout", hero)}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	generated, err := goClient(manifest)
	if err != nil {
		t.Fatal(err)
	}
	runGeneratedGoConsumer(t, generated, `package generated_test
import("encoding/json";"reflect";"testing";g "example.com/stable-consumer";"github.com/riducms/ridu/core")
func TestAuthoredRowKey(t *testing.T) {
 input:=&g.HeroLinksRowInput{Key:"identity",RowKeyField2:core.Set(g.HeroLinksRowRowKeyField2Input{Tone:core.Set(g.HeroLinksRowRowKeyField2ToneDark)}),RowKeyField:core.Set("authored")}
 data,err:=json.Marshal(input);if err!=nil{t.Fatal(err)}
 var want any
 if err:=json.Unmarshal([]byte("{\"_key\":\"identity\",\"rowKey\":{\"tone\":\"dark\"},\"rowKeyField\":\"authored\"}"),&want);err!=nil{t.Fatal(err)}
 for _,row:=range []interface{RowKey() string}{&g.HeroLinksRow{},&g.HeroLinksRowInput{},&g.HeroLinksRowUpdate{},&g.HeroLinksRowAllLocales{},&g.HeroLinksRowAllLocalesValue{}} {
  if err:=json.Unmarshal(data,row);err!=nil{t.Fatal(err)}
  if row.RowKey()!="identity"{t.Fatal("authored rowKey replaced identity")}
  encoded,err:=json.Marshal(row);if err!=nil{t.Fatal(err)}
  var got any;if err:=json.Unmarshal(encoded,&got);err!=nil{t.Fatal(err)}
  if !reflect.DeepEqual(want,got){t.Fatalf("authored field lost or renamed on wire: %s",encoded)}
 }
 plain:=g.HeroPlainUpdate{&g.HeroPlainRowInput{Key:"plain",RowKeyField:core.Set("authored")}}
 data,err=json.Marshal(plain);if err!=nil{t.Fatal(err)}
 var decoded g.HeroPlainUpdate;if err:=json.Unmarshal(data,&decoded);err!=nil{t.Fatal(err)}
 if decoded[0].RowKey()!="plain"{t.Fatal("plain row identity lost")}
 if value,_:=decoded[0].(*g.HeroPlainRowUpdate).RowKeyField.Get();value!="authored"{t.Fatal("scalar rowKey lost")}
 // Block fields do not reserve the array-only method name.
 _ = &g.HeroInput{RowKey:core.Set("authored block field")}
}
`)
}

const generatedBlocksErgonomicsConsumer = `package generated_test
import (
 "encoding/json"
 "errors"
 "reflect"
 "strings"
 "math"
 "testing"
 g "example.com/block-consumer"
 "github.com/riducms/ridu/core"
)

func TestRetain(t *testing.T) {
 heading := "do not copy"
 rows := g.PagesLayout{&g.Hero{Key:"hero", Heading:&heading}, &g.CTA{Key:"cta"}}
 if rows[0].BlockKey()!="hero" || rows[0].BlockType()!=g.HeroBlockType {t.Fatal("common identity contract")}
 updates,err:=rows.Retain();if err!=nil{t.Fatal(err)}
 encoded,err:=json.Marshal(updates);if err!=nil{t.Fatal(err)}
 var objects []map[string]any;if err:=json.Unmarshal(encoded,&objects);err!=nil{t.Fatal(err)}
 want:=[]map[string]any{{"blockType":"hero","_key":"hero"},{"blockType":"cta","_key":"cta"}}
 if !reflect.DeepEqual(objects,want){t.Fatalf("retention copied children or changed order: %s",encoded)}
 updates[0].(*g.HeroUpdate).Key="changed"
 if rows[0].BlockKey()!="hero"{t.Fatal("retention aliases original blocks")}
 // Constructed and decoded lists use the same dynamic pointer types.
 encoded,err=json.Marshal(rows);if err!=nil{t.Fatal(err)}
 var decoded g.PagesLayout;if err:=json.Unmarshal(encoded,&decoded);err!=nil{t.Fatal(err)}
 for i:=range rows {if reflect.TypeOf(rows[i])!=reflect.TypeOf(decoded[i]){t.Fatal("pointer convention differs after decoding")}}

 empty:=g.PagesLayout{};kept,err:=empty.Retain();if err!=nil||kept==nil||len(kept)!=0{t.Fatal("explicit empty list lost")}
 var absent *g.PagesLayout;if kept,err:=absent.Retain();err==nil||kept!=nil{t.Fatal("absent field retained")}
 var nilHero *g.Hero
 for _,bad:=range []g.PagesLayout{nil,{nil},{nilHero},{&g.Hero{}},{&g.Hero{Key:" "}}, {&g.Hero{Key:"same"},&g.CTA{Key:"same"}}} {
  kept,err:=bad.Retain();var detail *g.ContractError
  if err==nil||kept!=nil||!errors.As(err,&detail){t.Fatalf("unsafe retention: %#v, %v",kept,err)}
 }
 all:=g.PagesLayoutAllLocales{&g.HeroAllLocales{Key:"localized"}}
 if kept,err:=all.Retain();err!=nil||kept[0].BlockKey()!="localized"{t.Fatal("all-locales retention")}
 contextual:=g.PagesTranslatedLayoutAllLocalesValue{&g.HeroAllLocalesValue{Key:"contextual"}}
 if kept,err:=contextual.Retain();err!=nil||kept[0].BlockKey()!="contextual"{t.Fatal("contextual retention")}
 nested:=g.HeroChildren{&g.Note{Key:"nested"}}
 if kept,err:=nested.Retain();err!=nil||kept[0].BlockKey()!="nested"{t.Fatal("nested retention")}
}

func TestSelectOptions(t *testing.T) {
 rows:=g.PagesLayoutInput{&g.HeroInput{Heading:"Hello",Tone:core.Set(g.HeroToneDark),Alignment:core.Set(g.HeroAlignmentStart),Tags:core.Set([]g.HeroTags{g.HeroTagsNews,g.HeroTagsEvents}),BlockKeyField:core.Set("authored")}}
 data,err:=json.Marshal(rows);if err!=nil{t.Fatal(err)}
 var objects []map[string]any;if err:=json.Unmarshal(data,&objects);err!=nil{t.Fatal(err)}
 if objects[0]["tone"]!="dark"||objects[0]["alignment"]!="start"||objects[0]["blockKey"]!="authored"{t.Fatalf("option or reserved field changed wire values: %s",data)}
 var decoded g.PagesLayoutInput;if err:=json.Unmarshal(data,&decoded);err!=nil{t.Fatal(err)}
 round,err:=json.Marshal(decoded);if err!=nil||string(round)!=string(data){t.Fatal("typed option roundtrip")}
 var output g.PagesLayout
 if err:=json.Unmarshal([]byte("[{\"blockType\":\"hero\",\"_key\":\"one\",\"tone\":\"dark\",\"tags\":[\"news\"]}]"),&output);err!=nil{t.Fatal(err)}
 hero:=output[0].(*g.Hero);tone,ok:=hero.Tone.Get();if !ok||tone!=g.HeroToneDark{t.Fatal("typed output option")}
 tags,ok:=hero.Tags.Get();if !ok||len(tags)!=1||tags[0]!=g.HeroTagsNews{t.Fatal("typed has-many output option")}
}

func TestNestedErrorPaths(t *testing.T) {
 tests:=[]struct{name,raw,path string}{
  {"nested group", ` + "`" + `[{"blockType":"hero","heading":"Hello","style":{"tone":42}}]` + "`" + `, "[0].style.tone"},
  {"nested array", ` + "`" + `[{"blockType":"hero","heading":"Hello","links":[{"label":"ok"},{"label":null}]}]` + "`" + `, "[0].links[1].label"},
  {"nested block", ` + "`" + `[{"blockType":"hero","heading":"Hello","children":[{"blockType":"note","text":42}]}]` + "`" + `, "[0].children[0].text"},
  {"unknown nested field", ` + "`" + `[{"blockType":"hero","heading":"Hello","style":{"unknown":"private"}}]` + "`" + `, "[0].style.unknown"},
  {"missing identity", ` + "`" + `[{"blockType":"hero","heading":"Hello","_key":" "}]` + "`" + `, "[0]._key"},
  {"unknown discriminator", ` + "`" + `[{"blockType":"retired"}]` + "`" + `, "[0].blockType"},
 }
 for _,test:=range tests {t.Run(test.name,func(t *testing.T){
  original:=&g.CTAInput{Label:"unchanged"};rows:=g.PagesLayoutInput{original}
  err:=json.Unmarshal([]byte(test.raw),&rows);var detail *g.ContractError
  if !errors.As(err,&detail)||detail.Operation!="decode"||detail.Path!=test.path||detail.Container!="PagesLayoutInput" {t.Fatalf("path: %#v %v",detail,err)}
  if !strings.Contains(err.Error(),"decode PagesLayoutInput"+test.path+":")||strings.Contains(err.Error(),"private")||strings.Contains(err.Error(),"discriminator \"\""){t.Fatalf("diagnostic: %v",err)}
  if len(rows)!=1||rows[0]!=original{t.Fatal("failed decode changed destination")}
  if test.name=="nested group" {var mismatch *json.UnmarshalTypeError;if !errors.As(err,&mismatch)||!strings.Contains(err.Error(),"number"){t.Fatalf("lost JSON error: %v",err)}}
 })}
 var localized g.PagesLayoutAllLocales
 err:=json.Unmarshal([]byte(` + "`" + `[{"blockType":"hero","_key":"one","heading":{"en":"valid","fr":42}}]` + "`" + `),&localized)
 var detail *g.ContractError
 if !errors.As(err,&detail)||detail.Path!="[0].heading.fr"{t.Fatalf("locale path: %#v %v",detail,err)}
 var update g.PagesLayoutUpdate
 err=json.Unmarshal([]byte(` + "`" + `[{"blockType":"hero","_key":"one","links":[{"_key":"link","label":null}]}]` + "`" + `),&update)
 if !errors.As(err,&detail)||detail.Path!="[0].links[0].label"{t.Fatalf("update path: %#v %v",detail,err)}
 _,err=json.Marshal(g.PagesLayoutInput{&g.HeroInput{Heading:"Hello",Children:core.Set(g.HeroChildrenInput{nil})}})
 if !errors.As(err,&detail)||detail.Operation!="encode"||detail.Path!="[0].children[0]"{t.Fatalf("encode path: %#v %v",detail,err)}
 _,err=json.Marshal(g.PagesLayoutUpdate{&g.HeroUpdate{}})
 if !errors.As(err,&detail)||detail.Operation!="encode"||detail.Path!="[0]._key"{t.Fatalf("key encode path: %#v %v",detail,err)}
 rows:=g.PagesLayout{&g.Hero{Key:"same"},&g.CTA{Key:"same"}}
 _,err=rows.Retain()
 if !errors.As(err,&detail)||detail.Operation!="retain"||detail.Path!="[1]"||strings.Contains(err.Error(),"discriminator"){t.Fatalf("retain diagnostic: %#v %v",detail,err)}
}

func TestReviewedErrorBoundaries(t *testing.T) {
 tests:=[]struct{raw,path string}{
  {` + "`" + `[{"blockType":"hero","_key":"one","target":{"id":"p","layout":[{"blockType":"retired","_key":"nested"}]}}]` + "`" + `,"[0].target.layout[0].blockType"},
  {` + "`" + `[{"blockType":"hero","_key":"one","link":{"relationTo":"campaigns","id":{"id":"c","content":{"layout":[{"blockType":"retired","_key":"nested"}]}}}}]` + "`" + `,"[0].link.id.content.layout[0].blockType"},
  {` + "`" + `[{"blockType":"hero","_key":42}]` + "`" + `,"[0]._key"},
  {` + "`" + `[{"blockType":42,"_key":"one"}]` + "`" + `,"[0].blockType"},
  {` + "`" + `[{"blockType":"hero"}]` + "`" + `,"[0]._key"},
 }
 for _,test:=range tests {
  original:=&g.CTA{Key:"unchanged"};rows:=g.PagesLayout{original}
  err:=json.Unmarshal([]byte(test.raw),&rows);var detail *g.ContractError
  if !errors.As(err,&detail)||detail.Path!=test.path{t.Fatalf("path: want %s got %#v %v",test.path,detail,err)}
  if len(rows)!=1||rows[0]!=original{t.Fatal("failed population/header decode changed destination")}
 }
 var update g.PagesLayoutUpdate
 err:=json.Unmarshal([]byte(` + "`" + `[{"blockType":"hero","_key":"one","links":[{"_key":"valid","label":"ok"},{"_key":42,"label":"bad"}]}]` + "`" + `),&update)
 var detail *g.ContractError
 if !errors.As(err,&detail)||detail.Path!="[0].links[1]._key"{t.Fatalf("array header path: %#v %v",detail,err)}
 _,err=json.Marshal(g.PagesLayoutInput{&g.HeroInput{Heading:"Hello",Links:core.Set([]g.HeroLinksRowInput{{Label:"ok"},{Label:"bad",Amount:core.Set(math.NaN())}})}})
 var number *json.UnsupportedValueError
 if !errors.As(err,&detail)||detail.Path!="[0].links[1].amount"||detail.Operation!="encode"||!errors.As(err,&number){t.Fatalf("array encode: %#v %v",detail,err)}
 amounts:=map[string]*float64{"en":new(float64),"fr":new(float64)};*amounts["fr"]=math.NaN()
 _,err=json.Marshal(g.PagesLayoutAllLocales{&g.HeroAllLocales{Key:"one",Amount:&g.BlockOptional[map[string]*float64]{Value:&amounts}}})
 if !errors.As(err,&detail)||detail.Path!="[0].amount.fr"||detail.Operation!="encode"||!errors.As(err,&number){t.Fatalf("locale encode: %#v %v",detail,err)}
 // Explicit empties must not become absent when field-level codecs unpack wrappers.
 rows:=g.PagesLayoutInput{&g.HeroInput{Heading:"",Tags:core.Set([]g.HeroTags{}),Links:core.Set([]g.HeroLinksRowInput{}),Style:core.Set(g.HeroStyleInput{})}}
 data,err:=json.Marshal(rows);if err!=nil{t.Fatal(err)}
 var decoded g.PagesLayoutInput;if err:=json.Unmarshal(data,&decoded);err!=nil{t.Fatal(err)}
 round,err:=json.Marshal(decoded);if err!=nil||string(data)!=string(round)||!strings.Contains(string(data),` + "`" + `"links":[]` + "`" + `)||!strings.Contains(string(data),` + "`" + `"style":{}` + "`" + `){t.Fatalf("empty values changed: %s -> %s %v",data,round,err)}
}
`

const generatedArrayRetentionConsumer = `package generated_test
import("encoding/json";"errors";"testing";g "example.com/block-consumer")
func TestArrayRetention(t *testing.T) {
 label:="private read value"
 rows:=g.HeroLinks{{Key:"first",Label:&label},{Key:"second"}}
 retained,err:=rows.Retain();if err!=nil{t.Fatal(err)}
 if retained[0].RowKey()!="first"||retained[1].RowKey()!="second"{t.Fatal("retained identities require a type assertion")}
 data,err:=json.Marshal(retained);if err!=nil{t.Fatal(err)}
 if string(data)!="[{\"_key\":\"first\"},{\"_key\":\"second\"}]"{t.Fatalf("copied fields or lost order: %s",data)}
 retained[0].(*g.HeroLinksRowUpdate).Key="changed"
 if rows[0].Key!="first"{t.Fatal("retention aliases original rows")}
 retained=append(retained,&g.HeroLinksRowInput{Label:"new"})
 data,err=json.Marshal(retained);if err!=nil{t.Fatal(err)}
 var decoded g.HeroLinksUpdate
 if err:=json.Unmarshal(data,&decoded);err!=nil{t.Fatal(err)}
 if _,ok:=decoded[0].(*g.HeroLinksRowUpdate);!ok{t.Fatal("update pointer convention lost")}
 if _,ok:=decoded[2].(*g.HeroLinksRowInput);!ok{t.Fatal("new input pointer convention lost")}
 if decoded[0].RowKey()!="changed"||decoded[2].RowKey()!=""{t.Fatal("decoded update/input identities changed")}
 keyedInput:=g.HeroLinksUpdate{&g.HeroLinksRowInput{Key:"supplied",Label:"new"}}
 if keyedInput[0].RowKey()!="supplied"{t.Fatal("caller-supplied input identity lost")}
 var nilInput *g.HeroLinksRowInput
 var nilUpdate *g.HeroLinksRowUpdate
 for _,row:=range (g.HeroLinksUpdate{nilInput,nilUpdate}){if row.RowKey()!=""{t.Fatal("nil row identity is not empty")}}
 if _,err:=json.Marshal(g.HeroLinksUpdate{nilInput});err==nil{t.Fatal("nil row accepted")}
 empty:=g.HeroLinks{};kept,err:=empty.Retain();if err!=nil||kept==nil||len(kept)!=0{t.Fatal("empty became absent")}
 for _,bad:=range []g.HeroLinks{nil,{{Key:""}},{{Key:" "}},{{Key:"same"},{Key:"same"}}} {
  kept,err:=bad.Retain();var detail *g.ContractError
  if err==nil||kept!=nil||!errors.As(err,&detail)||detail.Operation!="retain"||detail.Container!="HeroLinks"{t.Fatalf("unsafe retention: %#v %v",kept,err)}
 }
 all:=g.HeroLinksAllLocales{{Key:"all"}};if kept,err:=all.Retain();err!=nil||kept[0].RowKey()!="all"||all[0].RowKey()!="all"{t.Fatal("all-locales retention")}
 contextual:=g.HeroLinksAllLocalesValue{{Key:"value"}};if kept,err:=contextual.Retain();err!=nil||kept[0].RowKey()!="value"||contextual[0].RowKey()!="value"{t.Fatal("localized-container retention")}
 // Malformed nested values cannot replace a successfully decoded list.
 err=json.Unmarshal([]byte("[{\"_key\":\"valid\",\"label\":\"ok\"},{\"_key\":\"bad\",\"label\":42}]"),&rows)
 var detail *g.ContractError
 if !errors.As(err,&detail)||detail.Path!="[1].label"||len(rows)!=2||rows[0].Key!="first"{t.Fatalf("array decode atomicity/path: %v",err)}
}
`

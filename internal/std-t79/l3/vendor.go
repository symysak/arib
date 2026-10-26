package l3

import "fmt"


type vendor struct {
	annotate func(m *Message, c Ctx)
}

var vendors = map[int]vendor{
	MakerJRC:     {annotate: annotateJRC},
	MakerHitachi: {annotate: annotateHitachi},
}

var vendorShort = map[int]string{MakerJRC: "日本無線", MakerHitachi: "日立国際電気"}

const (
	MakerJRC     = 163
	MakerHitachi = 171
)

func HasVendor(n int) bool { _, ok := vendors[n]; return ok }

func vendorTag(maker int) string {
	return "（独自・" + vendorShort[maker] + "・推定）"
}

func (m *Message) addVendorField(maker int, name, value string) {
	m.Fields = append(m.Fields, Field{name + vendorTag(maker), value})
}

func (m *Message) vendorItem(what string) *VendorItem {
	for i := range m.Vendor {
		if m.Vendor[i].What == what {
			return &m.Vendor[i]
		}
	}
	return nil
}

func (m *Message) renameUnknown(maker int, name string) {
	m.Name = fmt.Sprintf("%s（独自 0x%02X）", name, m.Type)
	m.Err = ""
	note := fmt.Sprintf("規格に無い種別 0x%02X（%sの独自とみる）", m.Type, vendorShort[maker])
	m.Notes = append(m.Notes, note)
	m.Fields = append(m.Fields, Field{"規格の検査", note})
	if m.Call != nil {
		m.Call.Text = ccCallText(m.Call, m.Name)
	}
}

func clockOf(h, mi, s int) int {
	if h < 0 || h > 23 || mi < 0 || mi > 59 || s < 0 || s > 59 {
		return -1
	}
	return h*3600 + mi*60 + s
}

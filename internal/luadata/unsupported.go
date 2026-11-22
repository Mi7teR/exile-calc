package luadata

// Unsupported lists PoB data files that are programs rather than data, with
// the reason. Their content is not exported.
var Unsupported = map[string]string{
	"Data/Uniques/Special/Generated.lua": "builds uniques (Megalomaniac, Forbidden jewels, keystone items) in loops over other data",
}

package iostore

import (
	"encoding/binary"
	"errors"
	"fmt"
	"slices"
)

// ErrKeyInUse reports a new key the package already reads elsewhere; replacing would merge the two inputs
// into one name that can no longer be told apart.
var ErrKeyInUse = errors.New("key already used by the package")

// Kismet bytecode of an FKey literal such as the argument of PlayerController.WasInputKeyJustPressed:
// EX_StructConst <struct import> <size> EX_NameConst <name index> <name number> EX_EndStructConst.
const (
	exStructConst    = 0x2F
	exNameConst      = 0x21
	exEndStructConst = 0x30
	keySiteSize      = 19
	keySiteName      = 10
)

var keyStructImport = scriptImport("/Script/InputCore.Key")

type keySite struct {
	offset int
	name   string
}

// InputKeys returns the distinct key names the package passes as FKey literals, in bytecode order. isKey
// limits them to names the caller can map.
func (p *Package) InputKeys(isKey func(string) bool) []string {
	var keys []string
	for _, site := range p.keySites(isKey) {
		if !slices.Contains(keys, site.name) {
			keys = append(keys, site.name)
		}
	}
	return keys
}

func (p *Package) keySites(isKey func(string) bool) []keySite {
	imports := p.imports()
	le := binary.LittleEndian
	data := p.data
	var sites []keySite
	for at := p.headerSize(); at+keySiteSize <= len(data); at++ {
		if data[at] != exStructConst || data[at+9] != exNameConst || data[at+18] != exEndStructConst {
			continue
		}
		ref := int32(le.Uint32(data[at+1:]))
		index := int(le.Uint32(data[at+keySiteName:]))
		if ref >= 0 || int(-ref-1) >= len(imports) || imports[-ref-1] != keyStructImport ||
			index >= len(p.names) || le.Uint32(data[at+keySiteName+4:]) != 0 || !isKey(p.names[index]) {
			continue
		}
		sites = append(sites, keySite{offset: at, name: p.names[index]})
	}
	return sites
}

// ReplaceInputKey returns the package bytes with every FKey literal of oldKey changed to newKey.
func (p *Package) ReplaceInputKey(oldKey, newKey string, isKey func(string) bool) ([]byte, error) {
	if !isKey(newKey) {
		return nil, fmt.Errorf("unknown key %q", newKey)
	}
	if slices.Contains(p.InputKeys(isKey), newKey) {
		return nil, fmt.Errorf("%w: %s", ErrKeyInUse, newKey)
	}
	target := p
	index := slices.Index(p.names, newKey)
	if index < 0 {
		var err error
		if target, err = p.withName(newKey); err != nil {
			return nil, err
		}
		index = len(target.names) - 1
	}

	var sites []keySite
	for _, site := range target.keySites(isKey) {
		if site.name == oldKey {
			sites = append(sites, site)
		}
	}
	if len(sites) == 0 {
		return nil, fmt.Errorf("key %q not found in package %s", oldKey, p.Name())
	}
	out := slices.Clone(target.data)
	for _, site := range sites {
		binary.LittleEndian.PutUint32(out[site.offset+keySiteName:], uint32(index))
	}
	return out, nil
}

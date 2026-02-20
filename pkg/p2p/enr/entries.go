// entries.go provides type-safe accessors for standard ENR entries as defined
// in EIP-778 and the Ethereum consensus specification. It includes IP, TCP,
// UDP, secp256k1, eth capability, attestation subnets, and sync committee
// subnet entries with proper serialization.
package enr

import (
	"encoding/binary"
	"errors"
	"net"
)

// Standard entry errors.
var (
	ErrEntryNotFound = errors.New("enr: entry not found")
	ErrInvalidEntry  = errors.New("enr: invalid entry value")
)

// --- IP entry ---

// IP returns the IPv4 address stored in the record, or nil if absent.
func IP(r *Record) net.IP {
	v := r.Get(KeyIP)
	if len(v) != 4 {
		return nil
	}
	ip := make(net.IP, 4)
	copy(ip, v)
	return ip
}

// SetIP stores an IPv4 address in the record.
func SetIP(r *Record, ip net.IP) {
	v := ip.To4()
	if v == nil {
		return
	}
	r.Set(KeyIP, v)
}

// IP6 returns the IPv6 address stored in the record, or nil if absent.
func IP6(r *Record) net.IP {
	v := r.Get(KeyIP6)
	if len(v) != 16 {
		return nil
	}
	ip := make(net.IP, 16)
	copy(ip, v)
	return ip
}

// SetIP6 stores an IPv6 address in the record.
func SetIP6(r *Record, ip net.IP) {
	v := ip.To16()
	if v == nil {
		return
	}
	r.Set(KeyIP6, v)
}

// --- TCP entry ---

// TCP returns the TCP port from the record, or 0 if absent.
func TCP(r *Record) uint16 {
	v := r.Get(KeyTCP)
	if len(v) < 2 {
		return 0
	}
	return binary.BigEndian.Uint16(v)
}

// SetTCP stores the TCP port in the record.
func SetTCP(r *Record, port uint16) {
	buf := make([]byte, 2)
	binary.BigEndian.PutUint16(buf, port)
	r.Set(KeyTCP, buf)
}

// TCP6 returns the IPv6 TCP port from the record, or 0 if absent.
func TCP6(r *Record) uint16 {
	v := r.Get(KeyTCP6)
	if len(v) < 2 {
		return 0
	}
	return binary.BigEndian.Uint16(v)
}

// SetTCP6 stores the IPv6 TCP port in the record.
func SetTCP6(r *Record, port uint16) {
	buf := make([]byte, 2)
	binary.BigEndian.PutUint16(buf, port)
	r.Set(KeyTCP6, buf)
}

// --- UDP entry ---

// UDP returns the UDP port from the record, or 0 if absent.
func UDP(r *Record) uint16 {
	v := r.Get(KeyUDP)
	if len(v) < 2 {
		return 0
	}
	return binary.BigEndian.Uint16(v)
}

// SetUDP stores the UDP port in the record.
func SetUDP(r *Record, port uint16) {
	buf := make([]byte, 2)
	binary.BigEndian.PutUint16(buf, port)
	r.Set(KeyUDP, buf)
}

// UDP6 returns the IPv6 UDP port from the record, or 0 if absent.
func UDP6(r *Record) uint16 {
	v := r.Get(KeyUDP6)
	if len(v) < 2 {
		return 0
	}
	return binary.BigEndian.Uint16(v)
}

// SetUDP6 stores the IPv6 UDP port in the record.
func SetUDP6(r *Record, port uint16) {
	buf := make([]byte, 2)
	binary.BigEndian.PutUint16(buf, port)
	r.Set(KeyUDP6, buf)
}

// --- Secp256k1 entry ---

// Secp256k1 returns the compressed secp256k1 public key from the record
// (33 bytes), or nil if absent.
func Secp256k1(r *Record) []byte {
	v := r.Get(KeySecp256k1)
	if len(v) != 33 {
		return nil
	}
	out := make([]byte, 33)
	copy(out, v)
	return out
}

// --- Eth capability entry ---

// ENR key for the eth capability (fork ID + next fork block).
const KeyEth = "eth"

// EthEntry represents the eth capability ENR entry. It encodes the
// fork hash and next fork block number used for chain compatibility checks.
type EthEntry struct {
	ForkHash [4]byte // CRC32 of the genesis hash + fork block numbers
	ForkNext uint64  // block number of the next expected fork (0 = none)
}

// SerializeEthEntry encodes an EthEntry into bytes: [4-byte hash][8-byte next].
func SerializeEthEntry(e *EthEntry) []byte {
	buf := make([]byte, 12)
	copy(buf[:4], e.ForkHash[:])
	binary.BigEndian.PutUint64(buf[4:], e.ForkNext)
	return buf
}

// DeserializeEthEntry decodes an EthEntry from bytes.
func DeserializeEthEntry(data []byte) (*EthEntry, error) {
	if len(data) < 12 {
		return nil, ErrInvalidEntry
	}
	e := &EthEntry{
		ForkNext: binary.BigEndian.Uint64(data[4:12]),
	}
	copy(e.ForkHash[:], data[:4])
	return e, nil
}

// Eth returns the eth capability entry from the record, or nil if absent.
func Eth(r *Record) *EthEntry {
	v := r.Get(KeyEth)
	if v == nil {
		return nil
	}
	e, err := DeserializeEthEntry(v)
	if err != nil {
		return nil
	}
	return e
}

// SetEth stores the eth capability entry in the record.
func SetEth(r *Record, e *EthEntry) {
	r.Set(KeyEth, SerializeEthEntry(e))
}

// --- Attestation subnets bitmap (consensus layer) ---

// KeyAttnets is the ENR key for the attestation subnet bitmap.
const KeyAttnets = "attnets"

// Attnets returns the 8-byte attestation subnet bitmap from the record,
// representing which of the 64 subnets the node subscribes to.
// Returns nil if absent or invalid.
func Attnets(r *Record) []byte {
	v := r.Get(KeyAttnets)
	if len(v) != 8 {
		return nil
	}
	out := make([]byte, 8)
	copy(out, v)
	return out
}

// SetAttnets stores the 8-byte attestation subnet bitmap.
func SetAttnets(r *Record, bitmap []byte) {
	if len(bitmap) != 8 {
		return
	}
	b := make([]byte, 8)
	copy(b, bitmap)
	r.Set(KeyAttnets, b)
}

// AttnetsSubscribed returns whether the node is subscribed to the given
// subnet index (0-63). Returns false if the bitmap is absent or the
// index is out of range.
func AttnetsSubscribed(r *Record, subnetIdx uint) bool {
	if subnetIdx >= 64 {
		return false
	}
	bm := r.Get(KeyAttnets)
	if len(bm) != 8 {
		return false
	}
	byteIdx := subnetIdx / 8
	bitIdx := subnetIdx % 8
	return bm[byteIdx]&(1<<bitIdx) != 0
}

// --- Sync committee subnets bitmap (consensus layer) ---

// KeySyncnets is the ENR key for the sync committee subnet bitmap.
const KeySyncnets = "syncnets"

// Syncnets returns the 1-byte sync committee subnet bitmap from the record,
// representing which of the 4 sync committee subnets the node subscribes to.
// Returns nil if absent or invalid.
func Syncnets(r *Record) []byte {
	v := r.Get(KeySyncnets)
	if len(v) != 1 {
		return nil
	}
	return []byte{v[0]}
}

// SetSyncnets stores the 1-byte sync committee subnet bitmap.
func SetSyncnets(r *Record, bitmap byte) {
	r.Set(KeySyncnets, []byte{bitmap})
}

// SyncnetsSubscribed returns whether the node is subscribed to the given
// sync committee subnet index (0-3).
func SyncnetsSubscribed(r *Record, subnetIdx uint) bool {
	if subnetIdx >= 4 {
		return false
	}
	v := r.Get(KeySyncnets)
	if len(v) != 1 {
		return false
	}
	return v[0]&(1<<subnetIdx) != 0
}

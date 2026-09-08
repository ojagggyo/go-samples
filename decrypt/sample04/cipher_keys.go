package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"strings"

	"github.com/joho/godotenv"
	"golang.org/x/crypto/ripemd160"
)

const base58Alphabet = "123456789ABCDEFGHJKLMNPQRSTUVWXYZabcdefghijkmnopqrstuvwxyz"

// ============================================================
// Base58 decode
// ============================================================

func base58Decode(s string) ([]byte, error) {
	if s == "" {
		return []byte{}, nil
	}

	result := []byte{0}

	for _, c := range s {
		index := strings.IndexRune(base58Alphabet, c)

		if index < 0 {
			return nil, fmt.Errorf(
				"invalid base58 character: %c",
				c,
			)
		}

		carry := index

		for j := 0; j < len(result); j++ {
			carry += int(result[j]) * 58
			result[j] = byte(carry & 0xff)
			carry >>= 8
		}

		for carry > 0 {
			result = append(
				result,
				byte(carry&0xff),
			)
			carry >>= 8
		}
	}

	// reverse
	for i, j := 0, len(result)-1; i < j; i, j = i+1, j-1 {
		result[i], result[j] =
			result[j], result[i]
	}

	// Base58 leading '1' -> zero byte
	zeros := 0

	for _, c := range s {
		if c != '1' {
			break
		}
		zeros++
	}

	if zeros > 0 {
		result = append(
			make([]byte, zeros),
			result...,
		)
	}

	return result, nil
}

// ============================================================
// Steem Public Key
//
// C++:
//
// public_key_type::public_key_type(
//     const std::string& base58str
// )
// {
//     std::string prefix(STEEM_ADDRESS_PREFIX);
//
//     auto bin = fc::from_base58(
//         base58str.substr(prefix_len)
//     );
//
//     auto bin_key =
//         fc::raw::unpack_from_vector<binary_key>(bin);
//
//     key_data = bin_key.data;
//
//     FC_ASSERT(
//         fc::ripemd160::hash(
//             key_data.data,
//             key_data.size()
//         )._hash[0] == bin_key.check
//     );
// }
//
// ============================================================

func steemPublicKeyToData(publicKey string) ([]byte, error) {

	var prefix string

	// 通常のSteemは STM
	if strings.HasPrefix(publicKey, "STM") {
		prefix = "STM"
	} else if strings.HasPrefix(publicKey, "STEEM") {
		prefix = "STEEM"
	} else {
		return nil, fmt.Errorf(
			"invalid public key prefix: %s",
			publicKey,
		)
	}

	encoded := publicKey[len(prefix):]

	decoded, err := base58Decode(encoded)
	if err != nil {
		return nil, err
	}

	// binary_key
	//
	// data  = 33 bytes
	// check = 4 bytes
	//
	// FC_REFLECT:
	//
	// FC_REFLECT(
	//   public_key_type::binary_key,
	//   (data)(check)
	// )

	if len(decoded) != 37 {
		return nil, fmt.Errorf(
			"decoded public key size = %d, want 37",
			len(decoded),
		)
	}

	keyData := decoded[:33]
	checkData := decoded[33:37]

	// RIPEMD-160
	h := ripemd160.New()

	_, err = h.Write(keyData)
	if err != nil {
		return nil, err
	}

	hash := h.Sum(nil)

	// fc::ripemd160::_hash は
	// 160bit = 20byte
	//
	// C++側の uint32_t _hash[5] の
	// 最初の値との比較に合わせるため、
	// little endianでuint32として扱う。
	expected := []byte{
		hash[0],
		hash[1],
		hash[2],
		hash[3],
	}

	if !equalBytes(
		checkData,
		expected,
	) {
		return nil, fmt.Errorf(
			"public key checksum mismatch: got=%x expected=%x",
			checkData,
			expected,
		)
	}

	return keyData, nil
}

// ============================================================
// byte compare
// ============================================================

func equalBytes(a, b []byte) bool {

	if len(a) != len(b) {
		return false
	}

	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}

	return true
}

// ============================================================
// FC unsigned_int
//
// C++:
//
// template<typename Stream>
// inline void pack(
//     Stream& s,
//     const unsigned_int& v
// )
// {
//     uint64_t val = v.value;
//
//     do {
//         uint8_t b = uint8_t(val) & 0x7f;
//         val >>= 7;
//         b |= ((val > 0) << 7);
//         s.write((char*)&b,1);
//     } while(val);
// }
// ============================================================

func writeUnsignedInt(v uint32) []byte {

	result := make([]byte, 0, 5)

	for {
		b := byte(v & 0x7f)

		v >>= 7

		if v != 0 {
			b |= 0x80
		}

		result = append(result, b)

		if v == 0 {
			break
		}
	}

	return result
}

// ============================================================
// plain_keys
//
// struct plain_keys
// {
//     fc::sha512 checksum;
//     map<public_key_type,string> keys;
// };
//
// FC_REFLECT:
//
// (checksum)(keys)
// ============================================================

func makePlainKeys(
	publicKey string,
	privateKey string,
	password string,
) ([]byte, error) {

	// --------------------------------------------------------
	// Public Key
	// --------------------------------------------------------

	publicKeyData, err :=
		steemPublicKeyToData(publicKey)

	if err != nil {
		return nil, err
	}

	if len(publicKeyData) != 33 {
		return nil, fmt.Errorf(
			"public key data size = %d, want 33",
			len(publicKeyData),
		)
	}

	// --------------------------------------------------------
	// Private Key
	// --------------------------------------------------------

	if len(privateKey) != 51 {
		return nil, fmt.Errorf(
			"private key length = %d, want 51",
			len(privateKey),
		)
	}

	// --------------------------------------------------------
	// SHA-512(password)
	//
	// C++:
	//
	// _checksum = fc::sha512::hash(password)
	//
	// --------------------------------------------------------

	checksum :=
		sha512.Sum512([]byte(password))

	data := make(
		[]byte,
		0,
		150,
	)

	// --------------------------------------------------------
	// checksum
	// 64 bytes
	// --------------------------------------------------------

	data = append(
		data,
		checksum[:]...,
	)

	// --------------------------------------------------------
	// map size
	// 1
	// --------------------------------------------------------

	data = append(
		data,
		writeUnsignedInt(1)...,
	)

	// --------------------------------------------------------
	// public_key_type
	//
	// FC_REFLECT(public_key_type, (key_data))
	//
	// key_data = 33 bytes
	// --------------------------------------------------------

	data = append(
		data,
		publicKeyData...,
	)

	// --------------------------------------------------------
	// string length
	// 51
	// --------------------------------------------------------

	data = append(
		data,
		writeUnsignedInt(
			uint32(len(privateKey)),
		)...,
	)

	// --------------------------------------------------------
	// private key
	// --------------------------------------------------------

	data = append(
		data,
		[]byte(privateKey)...,
	)

	return data, nil
}

// ============================================================
// PKCS#7 padding
// ============================================================

func pkcs7Pad(
	data []byte,
	blockSize int,
) []byte {

	padding :=
		blockSize -
			(len(data) % blockSize)

	result := make(
		[]byte,
		len(data)+padding,
	)

	copy(result, data)

	for i := len(data); i < len(result); i++ {
		result[i] = byte(padding)
	}

	return result
}

// ============================================================
// Steem AES encryption
//
// password
//    ↓
// SHA-512
//
// key = SHA512[0:32]
// IV  = SHA512[32:48]
//
// AES-256-CBC
// ============================================================

func encryptSteem(
	plainText []byte,
	password string,
) ([]byte, error) {

	hash :=
		sha512.Sum512([]byte(password))

	// AES-256 key
	key := hash[0:32]

	// IV
	iv := hash[32:48]

	block, err :=
		aes.NewCipher(key)

	if err != nil {
		return nil, err
	}

	// AES-CBC
	padded :=
		pkcs7Pad(
			plainText,
			aes.BlockSize,
		)

	cipherText :=
		make([]byte, len(padded))

	mode :=
		cipher.NewCBCEncrypter(
			block,
			iv,
		)

	mode.CryptBlocks(
		cipherText,
		padded,
	)

	return cipherText, nil
}

// ============================================================
// wallet.json
// ============================================================

type WalletJSON struct {
	CipherKeys string `json:"cipher_keys"`
	WSServer   string `json:"ws_server"`
	WSUser     string `json:"ws_user"`
	WSPassword string `json:"ws_password"`
}

// ============================================================
// main
// ============================================================

func main() {

	// ========================================================
	// ここを自分の値に変更
	// ========================================================

	// .envを読み込む
	err := godotenv.Load()
	if err != nil {
		panic(err)
	}

	// 環境変数から取得
	password := os.Getenv("WALLET_PASSWORD")
	publicKey := os.Getenv("PUBLICKEY")
	privateKey := os.Getenv("PRIVATEKEY")
	if password == "" {
		panic("STEEM_PASSWORD is not set")
	}
	if publicKey == "" {
		panic("STEEM_PUBLIC_KEY is not set")
	}
	if privateKey == "" {
		panic("STEEM_PRIVATE_KEY is not set")
	}

	// ========================================================
	// Public Key
	// ========================================================

	fmt.Println(
		"Public Key length:",
		len(publicKey),
	)

	publicKeyData, err :=
		steemPublicKeyToData(publicKey)

	if err != nil {
		panic(err)
	}

	fmt.Println(
		"Public Key data:",
		len(publicKeyData),
		"bytes",
	)

	fmt.Printf(
		"Public Key data: %x\n",
		publicKeyData,
	)

	// ========================================================
	// plain_keys
	// ========================================================

	plainKeys, err :=
		makePlainKeys(
			publicKey,
			privateKey,
			password,
		)

	if err != nil {
		panic(err)
	}

	fmt.Println(
		"plain_keys:",
		len(plainKeys),
		"bytes",
	)
	fmt.Printf("checksum = %x\n", plainKeys[:64])
	fmt.Printf("map count byte = %02x\n", plainKeys[64])

	fmt.Printf(
		"plain_keys: %x\n",
		plainKeys,
	)

	// ========================================================
	// AES
	// ========================================================

	cipherKeys, err :=
		encryptSteem(
			plainKeys,
			password,
		)

	if err != nil {
		panic(err)
	}

	// ========================================================
	// cipher_keys
	// ========================================================

	cipherKeysHex :=
		hex.EncodeToString(cipherKeys)

	fmt.Println()
	fmt.Println(
		"cipher_keys bytes:",
		len(cipherKeys),
	)

	fmt.Println(
		"cipher_keys chars:",
		len(cipherKeysHex),
	)

	fmt.Println()
	fmt.Println("cipher_keys:")
	fmt.Println(cipherKeysHex)

	// ========================================================
	// wallet.json
	// ========================================================

	wallet := WalletJSON{
		CipherKeys: cipherKeysHex,
		WSServer:   "ws://localhost:8090",
		WSUser:     "",
		WSPassword: "",
	}

	jsonData, err :=
		json.MarshalIndent(
			wallet,
			"",
			"  ",
		)

	if err != nil {
		panic(err)
	}

	err =

		os.WriteFile(
			"wallet.sample.json", // 本来はwallet.json
			jsonData,
			0600,
		)

	if err != nil {
		panic(err)
	}

	fmt.Println()
	fmt.Println(

		"wallet.json written.",
	)

}

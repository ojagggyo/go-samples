package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha512"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/joho/godotenv"
)

// ------------------------------------------------------------
// wallet.json
// ------------------------------------------------------------

type WalletJSON struct {
	CipherKeys string `json:"cipher_keys"`
	WSServer   string `json:"ws_server"`
	WSUser     string `json:"ws_user"`
	WSPassword string `json:"ws_password"`
}

// ------------------------------------------------------------
// FC unsigned_int
//
// fc::raw::pack(unsigned_int)
//
// 7bit variable-length integer
// ------------------------------------------------------------

func readUnsignedInt(data []byte, pos *int) (uint32, error) {
	var result uint32
	var shift uint

	for i := 0; i < 5; i++ {
		if *pos >= len(data) {
			return 0, errors.New("unexpected end while reading unsigned_int")
		}

		b := data[*pos]
		*pos++

		result |= uint32(b&0x7f) << shift

		if b&0x80 == 0 {
			return result, nil
		}

		shift += 7
	}

	return 0, errors.New("invalid unsigned_int")
}

// ------------------------------------------------------------
// AES-256-CBC
//
// Steem fc::aes:
//
// SHA512(password)
//
//   [0..31]   AES key
//   [32..47]  IV
//
// ------------------------------------------------------------

func decryptCipherKeys(cipherText []byte, password string) ([]byte, error) {

	hash := sha512.Sum512([]byte(password))

	// AES-256 key = first 32 bytes
	key := hash[0:32]

	// IV = bytes 32..47
	iv := hash[32:48]

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("AES: %w", err)
	}

	if len(cipherText)%aes.BlockSize != 0 {
		return nil, fmt.Errorf(
			"ciphertext length %d is not multiple of AES block size",
			len(cipherText),
		)
	}

	plain := make([]byte, len(cipherText))

	mode := cipher.NewCBCDecrypter(block, iv)
	mode.CryptBlocks(plain, cipherText)

	return plain, nil
}

// ------------------------------------------------------------
// PKCS#7 padding removal
//
// AES-CBC encrypted data produced by the Steem implementation
// can contain PKCS#7 padding.
//
// ------------------------------------------------------------

func removePKCS7(data []byte) ([]byte, error) {

	if len(data) == 0 {
		return nil, errors.New("empty plaintext")
	}

	padding := int(data[len(data)-1])

	if padding == 0 || padding > aes.BlockSize {
		return nil, errors.New("invalid PKCS#7 padding")
	}

	if padding > len(data) {
		return nil, errors.New("invalid padding length")
	}

	for i := len(data) - padding; i < len(data); i++ {
		if int(data[i]) != padding {
			return nil, errors.New("invalid PKCS#7 padding")
		}
	}

	return data[:len(data)-padding], nil
}

// ------------------------------------------------------------
// checksum
//
// plain_keys:
//
// struct plain_keys
// {
//     fc::sha512 checksum;
//     map<public_key_type,string> keys;
// };
//
// checksum = 64 bytes
// ------------------------------------------------------------

func readChecksum(data []byte, pos *int) ([]byte, error) {

	if len(data)-*pos < 64 {
		return nil, errors.New("not enough data for checksum")
	}

	checksum := make([]byte, 64)
	copy(checksum, data[*pos:*pos+64])

	*pos += 64

	return checksum, nil
}

// ------------------------------------------------------------
// public_key_type
//
// FC_REFLECT(public_key_type, (key_data))
//
// key_data = fc::ecc::public_key_data
// public_key_data = fc::array<char,33>
//
// therefore 33 bytes.
// ------------------------------------------------------------

func readPublicKey(data []byte, pos *int) ([]byte, error) {

	const publicKeySize = 33

	if len(data)-*pos < publicKeySize {
		return nil, errors.New("not enough data for public key")
	}

	key := make([]byte, publicKeySize)

	copy(key, data[*pos:*pos+publicKeySize])

	*pos += publicKeySize

	return key, nil
}

// ------------------------------------------------------------
// string
//
// fc::raw::pack(fc::string)
//
// unsigned_int(size)
// followed by raw bytes
// ------------------------------------------------------------

func readFCString(data []byte, pos *int) (string, error) {

	length, err := readUnsignedInt(data, pos)
	if err != nil {
		return "", err
	}

	if int(length) > len(data)-*pos {
		return "", errors.New("string length exceeds plaintext")
	}

	s := string(data[*pos : *pos+int(length)])

	*pos += int(length)

	return s, nil
}

// ------------------------------------------------------------
// plain_keys
//
// checksum
// map size
//   public_key_type
//   string
// ------------------------------------------------------------

type KeyEntry struct {
	PublicKey  []byte
	PrivateKey string
}

func parsePlainKeys(data []byte) ([]byte, []KeyEntry, error) {

	pos := 0

	// --------------------------------------------------------
	// checksum
	// --------------------------------------------------------

	checksum, err := readChecksum(data, &pos)
	if err != nil {
		return nil, nil, err
	}

	// --------------------------------------------------------
	// map size
	// --------------------------------------------------------

	count, err := readUnsignedInt(data, &pos)
	if err != nil {
		return nil, nil, err
	}

	// 安全対策
	if count > 100000 {
		return nil, nil, fmt.Errorf("unreasonable key count: %d", count)
	}

	keys := make([]KeyEntry, 0, count)

	for i := uint32(0); i < count; i++ {

		// public_key_type
		publicKey, err := readPublicKey(data, &pos)
		if err != nil {
			return nil, nil, fmt.Errorf(
				"key %d: public key: %w",
				i,
				err,
			)
		}

		// string
		privateKey, err := readFCString(data, &pos)
		if err != nil {
			return nil, nil, fmt.Errorf(
				"key %d: private key: %w",
				i,
				err,
			)
		}

		keys = append(keys, KeyEntry{
			PublicKey:  publicKey,
			PrivateKey: privateKey,
		})
	}

	return checksum, keys, nil
}

// ------------------------------------------------------------
// main
// ------------------------------------------------------------

func main() {

	if len(os.Args) < 2 {
		fmt.Println("Usage:")
		fmt.Println("  go run aaa.go wallet.json")
		fmt.Println()
		fmt.Println("Example:")
		fmt.Println("  go run aaa.go wallet.json")
		os.Exit(1)
	}
	walletFile := os.Args[1]

	// .envを読み込む
	err := godotenv.Load()
	if err != nil {
		panic(err)
	}
	// 環境変数から取得
	password := os.Getenv("WALLET_PASSWORD")

	// --------------------------------------------------------
	// wallet.json
	// --------------------------------------------------------

	jsonData, err := os.ReadFile(walletFile)
	if err != nil {
		fmt.Printf("wallet.json read error: %v\n", err)
		os.Exit(1)
	}

	var wallet WalletJSON

	if err := json.Unmarshal(jsonData, &wallet); err != nil {
		fmt.Printf("wallet.json parse error: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("jsonData length    = %d\n", len(jsonData))
	fmt.Printf("CipherKeys length  = %d\n", len(wallet.CipherKeys))

	if wallet.CipherKeys == "" {
		fmt.Println("cipher_keys is empty")
		os.Exit(1)
	}

	fmt.Printf("cipher_keys string length : %d\n", len(wallet.CipherKeys))

	// --------------------------------------------------------
	// cipher_keys
	// --------------------------------------------------------

	cipherText, err := hex.DecodeString(wallet.CipherKeys)
	if err != nil {
		fmt.Printf("cipher_keys hex decode error: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("ciphertext bytes         : %d\n", len(cipherText))

	// --------------------------------------------------------
	// AES decrypt
	// --------------------------------------------------------

	plaintext, err := decryptCipherKeys(cipherText, password)
	if err != nil {
		fmt.Printf("decrypt error: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("plaintext bytes          : %d\n", len(plaintext))
	fmt.Printf("plaintext hex            : %x\n", plaintext)

	// --------------------------------------------------------
	// PKCS#7 padding
	// --------------------------------------------------------

	unpadded, err := removePKCS7(plaintext)
	if err != nil {
		fmt.Printf("padding error: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("unpadded bytes            : %d\n", len(unpadded))

	// --------------------------------------------------------
	// plain_keys
	// --------------------------------------------------------

	checksum, keys, err := parsePlainKeys(unpadded)
	if err != nil {
		fmt.Printf("plain_keys parse error: %v\n", err)
		os.Exit(1)
	}

	fmt.Printf("checksum                  : %x\n", checksum)
	fmt.Printf("key count                 : %d\n", len(keys))

	// --------------------------------------------------------
	// keys
	// --------------------------------------------------------

	for i, key := range keys {

		fmt.Printf("\nKey #%d\n", i+1)

		fmt.Printf(
			"public key (%d bytes)     : %x\n",
			len(key.PublicKey),
			key.PublicKey,
		)

		// 秘密鍵そのものは表示しない
		/*fmt.Printf(
			"private key               : %d bytes, WIF=%v\n",
			len(key.PrivateKey),
			isLikelyWIF(key.PrivateKey),
		)*/
		fmt.Printf(
			"private key               : %s\n",
			key.PrivateKey,
		)
	}

	// --------------------------------------------------------
	// checksum verification
	//
	// wallet.cpp:
	//
	// pk.checksum == SHA512(password)
	//
	// --------------------------------------------------------

	passwordHash := sha512.Sum512([]byte(password))

	if string(checksum) == string(passwordHash[:]) {
		fmt.Println("\nchecksum verification     : OK")
	} else {
		fmt.Println("\nchecksum verification     : FAILED")
		fmt.Println("password is probably incorrect")
	}
}

// ------------------------------------------------------------
// WIFの簡易判定
// ------------------------------------------------------------

func isLikelyWIF(s string) bool {

	if len(s) < 50 || len(s) > 60 {
		return false
	}

	// Steemの通常のWIFは5から始まることが多い
	if len(s) > 0 && s[0] == '5' {
		return true
	}

	return false
}

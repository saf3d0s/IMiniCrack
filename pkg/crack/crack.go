package crack

import (
	"IMiniCrack/pkg/util"
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha1"
	"encoding/binary"
	"errors"
	"github.com/wailsapp/wails"
	"github.com/wailsapp/wails/v2/pkg/runtime"
	"golang.org/x/crypto/pbkdf2"
	"io"
	"log"
	"os"
	"strings"
)

type Crack struct {
	ctx context.Context
	rt  *wails.Runtime
	log *wails.CustomLogger
}

type WxapkgFile struct {
	NameLen int
	Name    string
	Offset  int
	Size    int
}

type SliceFile struct {
	Name string
	Data string
}

func (c *Crack) GetCtx(ctx context.Context) {
	c.ctx = ctx
}

// WailsInit .
func (c *Crack) WailsInit(runtime *wails.Runtime) error {
	c.log = c.rt.Log.New("Crack")
	c.rt = runtime
	return nil
}

func (c *Crack) Unpack(wxpkgPath, wxid, outPath string) string {
	if wxpkgPath == "" || wxid == "" {
		return "参数为空"
	}
	//c.log.Info("123123123")
	decData, err := c.decWxApkg(wxpkgPath, wxid)
	if err != nil {
		return err.Error()
	}
	err = c.unPackFile(wxpkgPath, decData, outPath, wxid)
	if err != nil {
		return err.Error()
	}

	return "解密导出成功"
}

func (c *Crack) decWxApkg(wxapkgPath string, wxid string) ([]byte, error) {
	salt := "saltiest"
	iv := "the iv: 16 bytes"
	dataByte, err := os.ReadFile(wxapkgPath)
	if err != nil {
		return nil, err
	}

	dk := pbkdf2.Key([]byte(wxid), []byte(salt), 1000, 32, sha1.New)
	block, _ := aes.NewCipher(dk)
	blockMode := cipher.NewCBCDecrypter(block, []byte(iv))
	originData := make([]byte, 1024)
	blockMode.CryptBlocks(originData, dataByte[6:1024+6])

	afData := make([]byte, len(dataByte)-1024-6)
	var xorKey = byte(0x66)
	if len(wxid) >= 2 {
		xorKey = wxid[len(wxid)-2]
	}
	for i, b := range dataByte[1024+6:] {
		afData[i] = b ^ xorKey
	}

	originData = append(originData[:1023], afData...)

	return originData, nil
}

func (c *Crack) unPackFile(wxapkgPath string, data []byte, outRoot string, wxid string) error {

	//fmt.Println(wxapkgPath)
	wxPackName := util.GetFileName(wxapkgPath)
	//fmt.Println()
	r := bytes.NewReader(data)
	firstMark := make([]byte, 1)
	_, err := r.Read(firstMark)
	if err != nil {
		return err
	}
	infoTable := make([]byte, 4)
	_, err = r.Read(infoTable)
	if err != nil {
		return err
	}
	indexInfoLength := make([]byte, 4)
	_, err = r.Read(indexInfoLength)
	if err != nil {
		return err
	}
	bodyInfoLength := make([]byte, 4)
	_, err = r.Read(bodyInfoLength)
	if err != nil {
		return err
	}
	lastMark := make([]byte, 1)
	_, err = r.Read(lastMark)
	if err != nil {
		return err
	}
	if bytes.Compare(firstMark, []byte{0xBE}) != 0 || bytes.Compare(lastMark, []byte{0xED}) != 0 {
		log.Println("It seems that this is not a valid file or the wxid you provided is wrong")
		return errors.New("It seems that this is not a valid file or the wxid you provided is wrong")
	}

	fileCount := make([]byte, 4)
	_, err = r.Read(fileCount)
	if err != nil {
		return err
	}

	//read index
	fileList := []WxapkgFile{}
	var i uint32 = 0
	for ; i < binary.BigEndian.Uint32(fileCount); i++ {
		line := WxapkgFile{}
		nameLen := make([]byte, 4)
		r.Read(nameLen)
		line.NameLen = int(binary.BigEndian.Uint32(nameLen))

		name := make([]byte, line.NameLen)
		r.Read(name)
		line.Name = string(name)

		offset := make([]byte, 4)
		r.Read(offset)
		line.Offset = int(binary.BigEndian.Uint32(offset))

		size := make([]byte, 4)
		r.Read(size)
		line.Size = int(binary.BigEndian.Uint32(size))

		fileList = append(fileList, line)
	}

	//save file
	nameList := []string{}
	for _, v := range fileList {
		outFileName := v.Name
		outFilePath := outRoot + "\\" + wxid + "\\" + wxPackName + "\\" + outFileName
		nameList = append(nameList, outFilePath)

		parentDir := util.GetParentDirectory(outFilePath)
		if !util.PathExists(parentDir) {
			err := os.MkdirAll(parentDir, 0666)
			if err != nil {
				return err
			}
		}

		out, err := os.OpenFile(outFilePath, os.O_CREATE|os.O_RDWR, 0666)
		if err != nil {
			return err
		}

		runtime.EventsEmit(c.ctx, "log", outFilePath)

		r.Seek(int64(v.Offset), 0)
		buf := make([]byte, v.Size)
		r.Read(buf)
		out.Write(buf)
		out.Close()
	}

	appServiceJsPath := ""
	for _, v := range nameList {
		if strings.Contains(v, "app-service.js") {
			appServiceJsPath = v
		}
	}
	//fix js
	fp_appServiceJs, err := os.OpenFile(appServiceJsPath, os.O_RDWR, 0666)
	if err != nil {
		return err
	}

	serverdata, err := io.ReadAll(fp_appServiceJs)
	if err != nil {
		return err
	}
	parseData := strings.Split(string(serverdata), "define(\"")
	//wxmlData := parseData[0]

	//fmt.Println(wxmlData)

	sliceList := []SliceFile{}
	for _, slice := range parseData[1:] {
		line := SliceFile{}
		arr := strings.SplitN(slice, "\",", 2)
		line.Name = arr[0]
		line.Data = arr[1][:strings.LastIndexAny(arr[1], "});")+1]
		sliceList = append(sliceList, line)
	}

	for _, sfile := range sliceList {
		outFilePath := outRoot + "\\" + wxid + "\\" + wxPackName + "\\" + sfile.Name

		parentDir := util.GetParentDirectory(outFilePath)
		if !util.PathExists(parentDir) {
			err := os.MkdirAll(parentDir, 0666)
			if err != nil {
				return err
			}
		}

		out, err := os.OpenFile(outFilePath, os.O_CREATE|os.O_RDWR, 0666)
		if err != nil {
			return err
		}
		out.WriteString(sfile.Data)
		out.Close()
	}
	return nil
}

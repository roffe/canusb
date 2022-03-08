package t8

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"time"

	"github.com/avast/retry-go"
	"github.com/roffe/gocan"
	"github.com/roffe/gocan/pkg/gmlan"
	"github.com/roffe/gocan/pkg/model"
)

func (t *Client) StartBootloader(ctx context.Context, startAddress uint32) error {
	// 06368000102400
	payload := []byte{
		0x06, 0x36, 0x80,
		byte(startAddress >> 24),
		byte(startAddress >> 16),
		byte(startAddress >> 8),
		byte(startAddress),
	}

	resp, err := t.c.SendAndPoll(ctx, gocan.NewFrame(0x7E0, payload, gocan.ResponseRequired), t.defaultTimeout, 0x7E8)
	if err != nil {
		return err
	}
	if err := gmlan.CheckErr(resp); err != nil {
		return err
	}
	return nil
}

func (t *Client) UploadBootloader(ctx context.Context, callback model.ProgressCallback) error {
	gm := gmlan.New(t.c)

	time.Sleep(50 * time.Millisecond)

	if err := gm.RequestDownload(ctx, 0x7E0, 0x7E8, false); err != nil {
		return err
	}
	startAddress := 0x102400
	Len := 9996 / 238
	seq := byte(0x21)

	start := time.Now()

	if callback != nil {
		callback(-float64(Len))
		callback(float64(0))
		callback("Uploading bootloader")
	}

	r := bytes.NewReader(LegionBytes)
	p := 0
	for i := 0; i < Len; i++ {
		if p == 15 {
			gm.TesterPresentNoResponseAllowed()
			p = 0
		}
		if callback != nil {
			callback(float64(i + 1))
		}
		if err := gm.DataTransfer(ctx, 0xF0, startAddress, 0x7E0, 0x7E8); err != nil {
			return err
		}
		seq = 0x21
		for j := 0; j < 0x22; j++ {
			payload := make([]byte, 8)
			payload[0] = seq
			for x := 1; x < 8; x++ {
				b, err := r.ReadByte()
				if err != nil && err != io.EOF {
					return err
				}
				payload[x] = b
			}

			tt := gocan.Outgoing
			if j == 0x21 {
				tt = gocan.ResponseRequired
			}
			f := gocan.NewFrame(0x7E0, payload, tt)
			if err := t.c.Send(f); err != nil {
				return err
			}

			seq++
			if seq > 0x2F {
				seq = 0x20
			}
		}
		resp, err := t.c.Poll(ctx, t.defaultTimeout, 0x7E8)
		if err != nil {
			return err
		}
		if err := gmlan.CheckErr(resp); err != nil {
			log.Println(resp.String())
			return err
		}
		d := resp.Data()
		if d[0] != 0x01 || d[1] != 0x76 {
			return errors.New("invalid transfer data response")
		}

		startAddress += 0xEA
	}

	seq = 0x21

	if err := gm.DataTransfer(ctx, 0x0A, startAddress, 0x7E0, 0x7E8); err != nil {
		return err
	}

	payload := make([]byte, 8)
	payload[0] = seq
	for x := 1; x < 8; x++ {
		b, err := r.ReadByte()
		if err != nil && err != io.EOF {
			return err
		}
		payload[x] = b
	}
	f2 := gocan.NewFrame(0x7E0, payload, gocan.ResponseRequired)
	resp, err := t.c.SendAndPoll(ctx, f2, t.defaultTimeout, 0x7E8)
	if err != nil {
		return err
	}

	if err := gmlan.CheckErr(resp); err != nil {
		return err
	}

	d := resp.Data()
	if d[0] != 0x01 || d[1] != 0x76 {
		return errors.New("invalid transfer data response")
	}
	gm.TesterPresentNoResponseAllowed()

	startAddress += 0x06
	if callback != nil {
		callback(fmt.Sprintf("Done, took: %s", time.Since(start).String()))
	}

	return nil
}

func (t *Client) LegionPing(ctx context.Context) error {
	frame := gocan.NewFrame(0x7E0, []byte{0xEF, 0xBE, 0x00, 0x00, 0x00, 0x00, 0x33, 0x66}, gocan.ResponseRequired)
	resp, err := t.c.SendAndPoll(ctx, frame, t.defaultTimeout, 0x7E8)
	if err != nil {
		return errors.New("LegionPing: " + err.Error())
	}
	if err := gmlan.CheckErr(resp); err != nil {
		return errors.New("LegionPing: " + err.Error())
	}
	d := resp.Data()
	if d[0] == 0xDE && d[1] == 0xAD && d[2] == 0xF0 && d[3] == 0x0F {
		return nil
	}
	return errors.New("LegionPing: no response")
}

func (t *Client) LegionExit(ctx context.Context) error {
	payload := []byte{0x01, 0x20, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00}
	frame := gocan.NewFrame(0x7E0, payload, gocan.ResponseRequired)
	resp, err := t.c.SendAndPoll(ctx, frame, t.defaultTimeout, 0x7E8)
	if err != nil {
		return errors.New("LegionExit: " + err.Error())
	}
	if err := gmlan.CheckErr(resp); err != nil {
		return errors.New("LegionExit: " + err.Error())
	}
	d := resp.Data()
	if d[0] != 0x01 || (d[1] != 0x50 && d[1] != 0x60) {
		return errors.New("LegionExit: invalid response")
	}
	return nil
}

func (t *Client) LegionEnableHighSpeed(ctx context.Context) error {
	log.Println("Enable LegionHighSpeed")
	payload := []byte{0x02, 0xA5, 0x00, 0x00, 0x00, 0x00, 0x00, 0x10}
	frame := gocan.NewFrame(0x7E0, payload, gocan.ResponseRequired)
	resp, err := t.c.SendAndPoll(ctx, frame, t.defaultTimeout, 0x7E8)
	if err != nil {
		return err
	}
	d := resp.Data()
	if d[3] != 0x01 {
		return errors.New("/!\\Failed to enable highspeed Legion")
	}
	return nil
}

// Commands are as follows:
//
// command 00: Configure packet delay.
//   wish:
//   Delay ( default is 2000 )
//
// command 01: Full Checksum-32
//   wish:
//   00: Trionic 8.
//   01: Trionic 8; MCP.
//
// command 02: Trionic 8; md5.
// wish:
//   00: Full md5.
//   01: Partition 1.
// ..
// 09: Partition 9.
//   Oddballs:
//   10: Read from 0x00000 to last address of binary + 512 bytes
//   11: Read from 0x04000 to last address of binary + 512 bytes
//   12: Read from 0x20000 to last address of binary + 512 bytes
//
// command 03: Trionic 8 MCP; md5.
//   wish:
//   00: Full md5.
//   01: Partition 1.
//   ..
//   09: Partition 9 aka 'Shadow'.
//
// command 04: Start secondary bootloader
//   wish: None, just wish.
//
// command 05: Marry secondary processor
//   wish: None, just wish.
//
// Command 06: Read ADC pin
//   whish:
//   Which pin to read.
func (t *Client) LegionIDemand(ctx context.Context, command byte, wish uint16) ([]byte, error) {
	log.Println("Legion i demand")
	payload := []byte{0x02, 0xA5, command, 0x00, 0x00, 0x00, byte(wish >> 8), byte(wish)}
	frame := gocan.NewFrame(0x7E0, payload, gocan.ResponseRequired)

	var out []byte

	err := retry.Do(func() error {
		//log.Println(frame.String())
		resp, err := t.c.SendAndPoll(ctx, frame, t.defaultTimeout, 0x7E8)
		if err != nil {
			return err
		}
		//log.Println(resp.String())
		d := resp.Data()
		if (command == 2 || command == 3) && d[3] != 1 {
			return errors.New("MD5 not ready yet")
		}

		if (command == 2 || command == 3) && d[3] == 1 {
			b, _, err := t.readDataByLocalIdentifier(ctx, nil, true, 7, 0, 16)
			if err != nil {
				return err
			}
			//log.Printf("%d>> %X || %q", n, b, b)
			out = b
			return nil
		}

		return nil
	},
		retry.Context(ctx),
		retry.Attempts(10),
		retry.LastErrorOnly(true),
	)
	if err != nil {
		return out, err
	}

	return out, nil
}

package service

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"testing"
	"time"

	"github.com/pkg/errors"

	"github.com/icon-project/goloop/network"

	"github.com/icon-project/goloop/common"
	"github.com/icon-project/goloop/common/db"
	"github.com/icon-project/goloop/module"
	"github.com/icon-project/goloop/rpc"
)

const (
	testTransactionNum = 20
)

type JSONRPCResponse struct {
	Version string          `json:"jsonrpc"`
	Result  json.RawMessage `json:"result"`
}

type Wallet struct {
	url string
}

func (w *Wallet) Call(method string, params map[string]interface{}) ([]byte, error) {
	d := map[string]interface{}{
		"jsonrpc": "2.0",
		"id":      1,
		"method":  method,
	}
	if params != nil {
		d["params"] = params
	}
	req, err := json.Marshal(d)
	if err != nil {
		log.Println("Making request fails")
		log.Println("Data", d)
		return nil, err
	}
	resp, err := http.Post(w.url, "application/json", bytes.NewReader(req))
	if resp.StatusCode != 200 {
		return nil, errors.New(
			fmt.Sprintf("FAIL to call res=%d", resp.StatusCode))
	}

	var buf = make([]byte, 2048*1024)
	var bufLen, readed int = 0, 0

	for true {
		readed, _ = resp.Body.Read(buf[bufLen:])
		if readed < 1 {
			break
		}
		bufLen += readed
	}
	var r JSONRPCResponse
	err = json.Unmarshal(buf[0:bufLen], &r)
	if err != nil {
		log.Println("JSON Parse Fail")
		log.Println("JSON=", string(buf[0:bufLen]))
		return nil, err
	}
	return r.Result.MarshalJSON()
}

func (w *Wallet) GetBlockByHeight(h int) ([]byte, error) {
	p := map[string]interface{}{
		"height": fmt.Sprintf("0x%x", h),
	}
	return w.Call("icx_getBlockByHeight", p)
}

type blockV1Impl struct {
	Version            string             `json:"version"`
	PrevBlockHash      common.RawHexBytes `json:"prev_block_hash"`
	MerkleTreeRootHash common.RawHexBytes `json:"merkle_tree_root_hash"`
	Transactions       []*transaction     `json:"confirmed_transaction_list"`
	BlockHash          common.RawHexBytes `json:"block_hash"`
	Height             int64              `json:"height"`
	PeerID             string             `json:"peer_id"`
	TimeStamp          uint64             `json:"time_stamp"`
	Signature          common.Signature   `json:"signature"`
}

func ParseLegacy(b []byte) (module.TransactionList, error) {
	var blk = new(blockV1Impl)
	err := json.Unmarshal(b, blk)
	if err != nil {
		return nil, err
	}
	trs := make([]module.Transaction, len(blk.Transactions))
	for i, tx := range blk.Transactions {
		trs[i] = tx
	}
	return NewTransactionListV1FromSlice(trs), nil
}

type transitionCb struct {
	exeDone chan bool
}

func (ts *transitionCb) OnValidate(module.Transition, error) {
}

func (ts *transitionCb) OnExecute(module.Transition, error) {
	ts.exeDone <- true
}

type serviceChain struct {
	wallet module.Wallet
	nid    int

	database db.Database
	sm       module.ServiceManager
	bm       module.BlockManager
	cs       module.Consensus
	sv       rpc.JsonRpcServer
}

func (c *serviceChain) VoteListDecoder() module.VoteListDecoder {
	return nil
}

func (c *serviceChain) Database() db.Database {
	return c.database
}

func (c *serviceChain) Wallet() module.Wallet {
	return c.wallet
}

func (c *serviceChain) NID() int {
	return c.nid
}

func (c *serviceChain) Genesis() []byte {
	genesis :=
		`{
		  "accounts": [
			{
			  "name": "god",
			  "address": "hx5a05b58a25a1e5ea0f1d5715e1f655dffc1fb30a",
			  "balance": "0x2961fff8ca4a623278000000000000000"
			},
			{
			  "name": "treasury",
			  "address": "hx1000000000000000000000000000000000000000",
			  "balance": "0x0"
			}
		  ],
		  "message": "A rhizome has no beginning or end; it is always in the middle, between things, interbeing, intermezzo. The tree is filiation, but the rhizome is alliance, uniquely alliance. The tree imposes the verb \"to be\" but the fabric of the rhizome is the conjunction, \"and ... and ...and...\"This conjunction carries enough force to shake and uproot the verb \"to be.\" Where are you going? Where are you coming from? What are you heading for? These are totally useless questions.\n\n - Mille Plateaux, Gilles Deleuze & Felix Guattari\n\n\"Hyperconnect the world\"",
		  "validatorlist": [
			"hx100000000000000000000000000000000001234",
			"hx100000000000000000000000000000000012345"
		  ]
		}`
	return []byte(genesis)
}

func TestUnitService(t *testing.T) {
	// request transactions
	c := new(serviceChain)
	c.wallet = common.NewWallet()
	c.database = db.NewMapDB()
	nt := network.NewTransport("127.0.0.1:8081", c.wallet)
	nt.Listen()
	defer nt.Close()

	leaderServiceManager := NewManager(c, network.NewManager("default", nt, module.ROLE_VALIDATOR), nil)
	it, _ := leaderServiceManager.CreateInitialTransition(nil, nil, -1)
	parentTrs, _ := leaderServiceManager.ProposeGenesisTransition(it)
	cb := &transitionCb{make(chan bool)}
	parentTrs.Execute(cb)
	<-cb.exeDone
	leaderServiceManager.Finalize(parentTrs, module.FinalizeNormalTransaction|module.FinalizeResult)

	// request SendTransaction
	sendDone := make(chan bool)

	height := 1
	wallet := Wallet{"https://testwallet.icon.foundation/api/v3"}
	go func() {
		for i := 0; i < testTransactionNum; i++ {
			b, err := wallet.GetBlockByHeight(height)
			if err != nil {
				panic(err)
			}
			tl, err := ParseLegacy(b)
			if err != nil {
				panic(err)
			}
			for itr := tl.Iterator(); itr.Has(); itr.Next() {
				t, _, _ := itr.Get()
				leaderServiceManager.SendTransaction(t)
			}
		}
		sendDone <- true
	}()

	//run service manager for leader
	txListChan := make(chan module.TransactionList)
	var validatorResult []byte
	// propose transition
	go func() {
		exeDone := make(chan bool)
		for {
			transition, err := leaderServiceManager.ProposeTransition(parentTrs)
			if err != nil {
				log.Panicf("Failed to propose transition!, err = %s\n", err)
			}
			txList := transition.NormalTransactions()
			txListChan <- txList
			<-txListChan
			cb := &transitionCb{exeDone}
			transition.Execute(cb)
			<-cb.exeDone
			if bytes.Compare(transition.Result(), validatorResult) != 0 {
				panic("Failed to compare result ")
			}
			leaderServiceManager.Finalize(transition, module.FinalizeNormalTransaction|module.FinalizeResult)
			// get result then run below
			parentTrs = transition
			time.Sleep(10 * time.Millisecond)
		}
	}()

	// validator
	validatorCh := new(serviceChain)
	validatorCh.wallet = common.NewWallet()
	validatorCh.database = db.NewMapDB()

	nt2 := network.NewTransport("127.0.0.1:8082", c.wallet)
	nt2.Listen()
	defer nt2.Close()

	if err := nt.Dial("127.0.0.1:8081", "default"); err != nil {
		log.Panic("Failed")
	}
	validatorServiceManager := NewManager(validatorCh, network.NewManager("default", nt2, module.ROLE_VALIDATOR), nil)
	vit, _ := leaderServiceManager.CreateInitialTransition(nil, nil, -1)
	parentVTransition, _ := leaderServiceManager.ProposeGenesisTransition(vit)
	parentVTransition.Execute(cb)
	<-cb.exeDone
	leaderServiceManager.Finalize(parentVTransition, module.FinalizeNormalTransaction|module.FinalizeResult)
	go func() {
		exeDone := make(chan bool)
		for {
			txList := <-txListChan
			vTransition, err := validatorServiceManager.CreateTransition(parentVTransition, txList)
			if err != nil {
				log.Panicf("Failed to create transition for validator : %s", err)
			}
			cb := &transitionCb{exeDone}
			vTransition.Execute(cb)
			<-cb.exeDone
			validatorResult = vTransition.Result()
			validatorServiceManager.Finalize(vTransition, module.FinalizeNormalTransaction|module.FinalizeResult)
			parentVTransition = vTransition
			txListChan <- nil
		}
	}()
	<-sendDone
}

package consensus

import (
	"bytes"

	"github.com/icon-project/goloop/common"
	"github.com/icon-project/goloop/common/codec"
	"github.com/icon-project/goloop/common/crypto"
	"github.com/icon-project/goloop/module"
	"github.com/pkg/errors"
)

var vlCodec = codec.MP

type voteList struct {
	Round          int32
	BlockPartSetID *PartSetID
	Signatures     []common.Signature
}

func (vl *voteList) Verify(block module.Block) error {
	// TODO
	if block.Height() == 1 {
		if len(vl.Signatures) == 0 {
			return nil
		} else {
			return errors.Errorf("voters for height 1\n")
		}
	}
	msg := newVoteMessage()
	msg.Height = block.Height()
	msg.Round = vl.Round
	msg.Type = voteTypePrecommit
	msg.BlockID = block.ID()
	msg.BlockPartSetID = vl.BlockPartSetID
	validators := block.NextValidators()
	for i, sig := range vl.Signatures {
		msg.Signature = sig
		index := validators.IndexOf(msg.address())
		if index < 0 {
			logger.Println(msg)
			return errors.Errorf("bad voter %x at index %d in vote list", msg.address(), i)
		}
	}
	twoThirds := validators.Len() * 2 / 3
	if len(vl.Signatures) > twoThirds {
		return nil
	}
	return errors.Errorf("votes(%d) <= 2/3 of validators(%d)", len(vl.Signatures), validators.Len())
}

func (vl *voteList) Bytes() []byte {
	bs, err := vlCodec.MarshalToBytes(vl)
	if err != nil {
		return nil
	}
	return bs
}

func (vl *voteList) Hash() []byte {
	return crypto.SHA3Sum256(vl.Bytes())
}

func newVoteList(msgs []*voteMessage) *voteList {
	vl := &voteList{}
	l := len(msgs)
	if l > 0 {
		vl.Round = msgs[0].Round
		vl.BlockPartSetID = msgs[0].BlockPartSetID
		vl.Signatures = make([]common.Signature, l)
		blockID := msgs[0].BlockID
		for i := 0; i < l; i++ {
			vl.Signatures[i] = msgs[i].Signature
			if !bytes.Equal(blockID, msgs[i].BlockID) {
				logger.Panicf("newVoteList: bad block id in messages <%x> <%x>", blockID, msgs[i].BlockID)
			}
		}
	}
	return vl
}

// NewVoteListFromBytes returns VoteList from serialized bytes
func NewVoteListFromBytes(bs []byte) module.VoteList {
	vl := &voteList{}
	_, err := vlCodec.UnmarshalFromBytes(bs, vl)
	if err != nil {
		return nil
	}
	return vl
}

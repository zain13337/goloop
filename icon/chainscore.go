/*
 * Copyright 2020 ICON Foundation
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package icon

import (
	"encoding/json"
	"math/big"

	"github.com/icon-project/goloop/common"
	"github.com/icon-project/goloop/common/errors"
	"github.com/icon-project/goloop/common/log"
	"github.com/icon-project/goloop/icon/iiss"
	"github.com/icon-project/goloop/module"
	"github.com/icon-project/goloop/service/contract"
	"github.com/icon-project/goloop/service/platform/basic"
	"github.com/icon-project/goloop/service/scoreapi"
	"github.com/icon-project/goloop/service/scoredb"
	"github.com/icon-project/goloop/service/scoreresult"
	"github.com/icon-project/goloop/service/state"
)

type chainMethod struct {
	scoreapi.Method
	minVer, maxVer int
}

type chainScore struct {
	cc    contract.CallContext
	log   log.Logger
	from  module.Address
	value *big.Int
}

const (
	CIDForMainNet         = 0xaf4e97
	StatusIllegalArgument = module.StatusReverted + iota
	StatusNotFound
)

var chainMethods = []*chainMethod{
	{scoreapi.Method{scoreapi.Function, "setStake",
		scoreapi.FlagExternal | scoreapi.FlagPayable, 0,
		[]scoreapi.Parameter{
			{"value", scoreapi.Integer, nil, nil},
		},
		nil,
	}, 0, 0}, // TODO change minVer to Revision5
	{scoreapi.Method{scoreapi.Function, "getStake",
		scoreapi.FlagReadOnly | scoreapi.FlagExternal, 0,
		[]scoreapi.Parameter{
			{"address", scoreapi.Address, nil, nil},
		},
		[]scoreapi.DataType{
			scoreapi.Dict,
		},
	}, 0, 0}, // TODO change minVer to Revision5
	{scoreapi.Method{scoreapi.Function, "setDelegation",
		scoreapi.FlagExternal, 0,
		[]scoreapi.Parameter{
			{"delegations", scoreapi.ListTypeOf(1, scoreapi.Struct), nil,
				[]scoreapi.Field{
					{"address", scoreapi.String, nil},
					{"value", scoreapi.Integer, nil},
				},
			},
		},
		nil,
	}, 0, 0}, // TODO change minVer to Revision5
	{scoreapi.Method{scoreapi.Function, "getDelegation",
		scoreapi.FlagReadOnly | scoreapi.FlagExternal, 0,
		[]scoreapi.Parameter{
			{"address", scoreapi.Address, nil, nil},
		},
		[]scoreapi.DataType{
			scoreapi.Dict,
		},
	}, 0, 0}, // TODO change minVer to Revision5
	{scoreapi.Method{scoreapi.Function, "registerPRep",
		scoreapi.FlagPayable | scoreapi.FlagExternal, 0,
		[]scoreapi.Parameter{
			{"name", scoreapi.String, nil, nil},
			{"email", scoreapi.String, nil, nil},
			{"website", scoreapi.String, nil, nil},
			{"country", scoreapi.String, nil, nil},
			{"city", scoreapi.String, nil, nil},
			{"details", scoreapi.String, nil, nil},
			{"p2pEndpoint", scoreapi.String, nil, nil},
			{"nodeAddress", scoreapi.Address, nil, nil},
		},
		nil,
	}, 0, 0}, // TODO change minVer to Revision5
	{scoreapi.Method{scoreapi.Function, "getPRep",
		scoreapi.FlagReadOnly | scoreapi.FlagExternal, 0,
		[]scoreapi.Parameter{
			{"address", scoreapi.Address, nil, nil},
		},
		[]scoreapi.DataType{
			scoreapi.Dict,
		},
	}, 0, 0}, // TODO change minVer to Revision5
}

func applyStepLimits(as state.AccountState, limits map[string]int64) error {
	stepLimitTypes := scoredb.NewArrayDB(as, state.VarStepLimitTypes)
	stepLimitDB := scoredb.NewDictDB(as, state.VarStepLimit, 1)
	for _, k := range state.AllStepLimitTypes {
		cost, _ := limits[k]
		if err := stepLimitTypes.Put(k); err != nil {
			return err
		}
		if err := stepLimitDB.Set(k, cost); err != nil {
			return err
		}
	}
	return nil
}

func applyStepCosts(as state.AccountState, costs map[string]int64) error {
	stepTypes := scoredb.NewArrayDB(as, state.VarStepTypes)
	stepCostDB := scoredb.NewDictDB(as, state.VarStepCosts, 1)
	for _, k := range state.AllStepTypes {
		cost, _ := costs[k]
		if err := stepTypes.Put(k); err != nil {
			return err
		}
		if err := stepCostDB.Set(k, cost); err != nil {
			return err
		}
	}
	return nil
}

func applyStepPrice(as state.AccountState, price *big.Int) error {
	return scoredb.NewVarDB(as, state.VarStepPrice).Set(price)
}

func (s *chainScore) Install(param []byte) error {
	if s.from != nil {
		return scoreresult.AccessDeniedError.New("AccessDeniedToInstallChainSCORE")
	}

	chain := basic.Chain{}
	if param != nil {
		if err := json.Unmarshal(param, &chain); err != nil {
			return scoreresult.Errorf(module.StatusIllegalFormat, "Failed to parse parameter for chainScore. err(%+v)\n", err)
		}
	}

	// load validatorList
	// set block interval 2 seconds
	as := s.cc.GetAccountState(state.SystemID)
	if err := scoredb.NewVarDB(as, state.VarBlockInterval).Set(2000); err != nil {
		return err
	}

	// skip transaction
	if err := scoredb.NewVarDB(as, state.VarRoundLimitFactor).Set(3); err != nil {
		return err
	}

	stepLimitsMap := map[string]int64{}
	stepTypesMap := map[string]int64{}
	stepPrice := big.NewInt(0)

	switch s.cc.ChainID() {
	case CIDForMainNet:
		// initialize for main network
	default:
		stepLimitsMap = map[string]int64{
			state.StepLimitTypeInvoke: 0x9502f900,
			state.StepLimitTypeQuery:  0x2faf080,
		}
		stepTypesMap = map[string]int64{
			state.StepTypeDefault:          0x186a0,
			state.StepTypeContractCall:     0x61a8,
			state.StepTypeContractCreate:   0x3b9aca00,
			state.StepTypeContractUpdate:   0x5f5e1000,
			state.StepTypeContractDestruct: -0x11170,
			state.StepTypeContractSet:      0x7530,
			state.StepTypeGet:              0x0,
			state.StepTypeSet:              0x140,
			state.StepTypeReplace:          0x50,
			state.StepTypeDelete:           -0xf0,
			state.StepTypeInput:            0xc8,
			state.StepTypeEventLog:         0x64,
			state.StepTypeApiCall:          0x2710,
		}
		stepPrice = big.NewInt(0x2e90edd00)

		validators := make([]module.Validator, len(chain.ValidatorList))
		for i, validator := range chain.ValidatorList {
			validators[i], _ = state.ValidatorFromAddress(validator)
			s.log.Debugf("add validator %d: %v", i, validator)
		}
		if err := s.cc.GetValidatorState().Set(validators); err != nil {
			return errors.CriticalUnknownError.Wrap(err, "FailToSetValidators")
		}

		s.cc.GetExtensionState().Reset(iiss.NewExtensionSnapshot(s.cc.Database(), nil))
	}

	if err := applyStepLimits(as, stepLimitsMap); err != nil {
		return err
	}
	if err := applyStepCosts(as, stepTypesMap); err != nil {
		return err
	}
	if err := applyStepPrice(as, stepPrice); err != nil {
		return err
	}

	return nil
}

func (s *chainScore) Update(param []byte) error {
	return nil
}

func (s *chainScore) GetAPI() *scoreapi.Info {
	ass := s.cc.GetAccountSnapshot(state.SystemID)
	as := scoredb.NewStateStoreWith(ass)
	revision := int(scoredb.NewVarDB(as, state.VarRevision).Int64())
	mLen := len(chainMethods)
	methods := make([]*scoreapi.Method, mLen)
	j := 0
	for _, m := range chainMethods {
		if m.minVer <= revision && (m.maxVer == 0 || revision <= m.maxVer) {
			methods[j] = &m.Method
			j += 1
		}
	}

	return scoreapi.NewInfo(methods[:j])
}

func newChainScore(cc contract.CallContext, from module.Address, value *big.Int) (contract.SystemScore, error) {
	return &chainScore{cc: cc, from: from, value: value, log: cc.Logger()}, nil
}

func (s *chainScore) Ex_setStake(value *common.HexInt) error {
	es := s.cc.GetExtensionState()
	if err := iiss.NewHandler(s.cc, s.from, s.value, es).SetStake(&value.Int); err != nil {
		return scoreresult.Errorf(basic.StatusIllegalArgument, err.Error())
	}
	return nil
}

func (s *chainScore) Ex_getStake(address module.Address) (map[string]interface{}, error) {
	es := s.cc.GetExtensionState()
	return iiss.NewHandler(s.cc, s.from, s.value, es).GetStake(address)
}

func (s *chainScore) Ex_setDelegation(param []interface{}) error {
	es := s.cc.GetExtensionState()
	if err := iiss.NewHandler(s.cc, s.from, s.value, es).SetDelegation(param); err != nil {
		return scoreresult.Errorf(basic.StatusIllegalArgument, err.Error())
	}
	return nil
}

func (s *chainScore) Ex_getDelegation(address module.Address) (map[string]interface{}, error) {
	es := s.cc.GetExtensionState()
	return iiss.NewHandler(s.cc, s.from, s.value, es).GetDelegation(address)
}

func (s *chainScore) Ex_registerPRep(name string, email string, website string, country string, city string,
	details string, p2pEndpoint string, nodeAddress module.Address) error {
	es := s.cc.GetExtensionState()
	return iiss.NewHandler(s.cc, s.from, s.value, es).RegisterPRep(name, email, website, country, city,
		details, p2pEndpoint, nodeAddress)
}

func (s *chainScore) Ex_getPRep(address module.Address) (map[string]interface{}, error) {
	es := s.cc.GetExtensionState()
	return iiss.NewHandler(s.cc, s.from, s.value, es).GetPRep(address)
}

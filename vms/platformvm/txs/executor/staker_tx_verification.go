// Copyright (C) 2019-2023, Ava Labs, Inc. All rights reserved.
// See the file LICENSE for licensing terms.

package executor

import (
	"errors"
	"fmt"
	"math"
	"time"

	"github.com/ava-labs/avalanchego/database"
	"github.com/ava-labs/avalanchego/ids"
	"github.com/ava-labs/avalanchego/utils/constants"
	"github.com/ava-labs/avalanchego/vms/components/avax"
	"github.com/ava-labs/avalanchego/vms/platformvm/state"
	"github.com/ava-labs/avalanchego/vms/platformvm/txs"

	safemath "github.com/ava-labs/avalanchego/utils/math"
)

var (
	ErrWeightTooSmall                  = errors.New("weight of this validator is too low")
	ErrWeightTooLarge                  = errors.New("weight of this validator is too large")
	ErrInsufficientDelegationFee       = errors.New("staker charges an insufficient delegation fee")
	ErrStakeTooShort                   = errors.New("staking period is too short")
	ErrStakeTooLong                    = errors.New("staking period is too long")
	ErrFlowCheckFailed                 = errors.New("flow check failed")
	ErrFutureStakeTime                 = fmt.Errorf("staker is attempting to start staking more than %s ahead of the current chain time", MaxFutureStartTime)
	ErrNotValidator                    = errors.New("isn't a current or pending validator")
	ErrRemovePermissionlessValidator   = errors.New("attempting to remove permissionless validator")
	ErrStakeOverflow                   = errors.New("validator stake exceeds limit")
	ErrPeriodMismatch                  = errors.New("proposed staking period is not inside dependant staking period")
	ErrOverDelegated                   = errors.New("validator would be over delegated")
	ErrIsNotTransformSubnetTx          = errors.New("is not a transform subnet tx")
	ErrTimestampNotBeforeStartTime     = errors.New("chain timestamp not before start time")
	ErrAlreadyValidator                = errors.New("already a validator")
	ErrDuplicateValidator              = errors.New("duplicate validator")
	ErrDelegateToPermissionedValidator = errors.New("delegation to permissioned validator")
	ErrWrongStakedAssetID              = errors.New("incorrect staked assetID")
	ErrDUpgradeNotActive               = errors.New("attempting to use a D-upgrade feature prior to activation")
)

// verifySubnetValidatorPrimaryNetworkRequirements verifies the primary
// network requirements for [subnetValidator]. An error is returned if they
// are not fulfilled.
func verifySubnetValidatorPrimaryNetworkRequirements(backend *Backend, chainState state.Chain, subnetValidator txs.Validator) error {
	primaryNetworkValidator, err := GetValidator(chainState, constants.PrimaryNetworkID, subnetValidator.NodeID)
	if err == database.ErrNotFound {
		return fmt.Errorf(
			"%s %w of the primary network",
			subnetValidator.NodeID,
			ErrNotValidator,
		)
	}
	if err != nil {
		return fmt.Errorf(
			"failed to fetch the primary network validator for %s: %w",
			subnetValidator.NodeID,
			err,
		)
	}

	// Ensure that the period this validator validates the specified subnet
	// is a subset of the time they validate the primary network.
	var (
		currentChainTime = chainState.GetTimestamp()
		stakerStart      = subnetValidator.StartTime()
	)
	if backend.Config.IsDActivated(currentChainTime) {
		stakerStart = currentChainTime
	}
	if !txs.BoundedBy(
		stakerStart,
		subnetValidator.EndTime(),
		primaryNetworkValidator.StartTime,
		primaryNetworkValidator.EndTime,
	) {
		return ErrPeriodMismatch
	}

	return nil
}

// verifyAddValidatorTx carries out the validation for an AddValidatorTx.
// It returns the tx outputs that should be returned if this validator is not
// added to the staking set.
func verifyAddValidatorTx(
	backend *Backend,
	chainState state.Chain,
	sTx *txs.Tx,
	tx *txs.AddValidatorTx,
) (
	[]*avax.TransferableOutput,
	error,
) {
	// Verify the tx is well-formed
	if err := sTx.SyntacticVerify(backend.Ctx); err != nil {
		return nil, err
	}

	var (
		currentTimestamp = chainState.GetTimestamp()
		duration         = tx.EndTime().Sub(currentTimestamp)
		isDurangoActive  = backend.Config.IsDActivated(currentTimestamp)
	)
	if !isDurangoActive {
		duration = tx.EndTime().Sub(tx.StartTime())
	}

	switch {
	case tx.Validator.Wght < backend.Config.MinValidatorStake:
		// Ensure validator is staking at least the minimum amount
		return nil, ErrWeightTooSmall

	case tx.Validator.Wght > backend.Config.MaxValidatorStake:
		// Ensure validator isn't staking too much
		return nil, ErrWeightTooLarge

	case tx.DelegationShares < backend.Config.MinDelegationFee:
		// Ensure the validator fee is at least the minimum amount
		return nil, ErrInsufficientDelegationFee

	case duration < backend.Config.MinStakeDuration:
		// Ensure staking length is not too short
		return nil, ErrStakeTooShort

	case duration > backend.Config.MaxStakeDuration:
		// Ensure staking length is not too long
		return nil, ErrStakeTooLong
	}

	outs := make([]*avax.TransferableOutput, len(tx.Outs)+len(tx.StakeOuts))
	copy(outs, tx.Outs)
	copy(outs[len(tx.Outs):], tx.StakeOuts)

	if !backend.Bootstrapped.Get() {
		return outs, nil
	}

	// Ensure the proposed validator starts after the current time
	if err := verifyStakerStartTime(isDurangoActive, currentTimestamp, tx.StartTime()); err != nil {
		return nil, err
	}

	_, err := GetValidator(chainState, constants.PrimaryNetworkID, tx.Validator.NodeID)
	if err == nil {
		return nil, fmt.Errorf(
			"%s is %w of the primary network",
			tx.Validator.NodeID,
			ErrAlreadyValidator,
		)
	}
	if err != database.ErrNotFound {
		return nil, fmt.Errorf(
			"failed to find whether %s is a primary network validator: %w",
			tx.Validator.NodeID,
			err,
		)
	}

	// Verify the flowcheck
	if err := backend.FlowChecker.VerifySpend(
		tx,
		chainState,
		tx.Ins,
		outs,
		sTx.Creds,
		map[ids.ID]uint64{
			backend.Ctx.AVAXAssetID: backend.Config.AddPrimaryNetworkValidatorFee,
		},
	); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrFlowCheckFailed, err)
	}

	// verifyStakerStartsSoon is checked last to allow
	// the verifier visitor to explicitly check for this error.
	return outs, verifyStakerStartsSoon(backend, currentTimestamp, tx.StartTime())
}

// verifyAddSubnetValidatorTx carries out the validation for an
// AddSubnetValidatorTx.
func verifyAddSubnetValidatorTx(
	backend *Backend,
	chainState state.Chain,
	sTx *txs.Tx,
	tx *txs.AddSubnetValidatorTx,
) error {
	// Verify the tx is well-formed
	if err := sTx.SyntacticVerify(backend.Ctx); err != nil {
		return err
	}

	var (
		currentTimestamp = chainState.GetTimestamp()
		duration         = tx.EndTime().Sub(currentTimestamp)
		isDurangoActive  = backend.Config.IsDActivated(currentTimestamp)
	)
	if !isDurangoActive {
		duration = tx.EndTime().Sub(tx.StartTime())
	}

	switch {
	case duration < backend.Config.MinStakeDuration:
		// Ensure staking length is not too short
		return ErrStakeTooShort

	case duration > backend.Config.MaxStakeDuration:
		// Ensure staking length is not too long
		return ErrStakeTooLong
	}

	if !backend.Bootstrapped.Get() {
		return nil
	}

	// Ensure the proposed validator starts after the current time
	if err := verifyStakerStartTime(isDurangoActive, currentTimestamp, tx.StartTime()); err != nil {
		return err
	}

	_, err := GetValidator(chainState, tx.SubnetValidator.Subnet, tx.Validator.NodeID)
	if err == nil {
		return fmt.Errorf(
			"attempted to issue %w for %s on subnet %s",
			ErrDuplicateValidator,
			tx.Validator.NodeID,
			tx.SubnetValidator.Subnet,
		)
	}
	if err != database.ErrNotFound {
		return fmt.Errorf(
			"failed to find whether %s is a subnet validator: %w",
			tx.Validator.NodeID,
			err,
		)
	}

	if err := verifySubnetValidatorPrimaryNetworkRequirements(backend, chainState, tx.Validator); err != nil {
		return err
	}

	baseTxCreds, err := verifyPoASubnetAuthorization(backend, chainState, sTx, tx.SubnetValidator.Subnet, tx.SubnetAuth)
	if err != nil {
		return err
	}

	// Verify the flowcheck
	if err := backend.FlowChecker.VerifySpend(
		tx,
		chainState,
		tx.Ins,
		tx.Outs,
		baseTxCreds,
		map[ids.ID]uint64{
			backend.Ctx.AVAXAssetID: backend.Config.AddSubnetValidatorFee,
		},
	); err != nil {
		return fmt.Errorf("%w: %w", ErrFlowCheckFailed, err)
	}

	// verifyStakerStartsSoon is checked last to allow
	// the verifier visitor to explicitly check for this error.
	return verifyStakerStartsSoon(backend, currentTimestamp, tx.StartTime())
}

// Returns the representation of [tx.NodeID] validating [tx.Subnet].
// Returns true if [tx.NodeID] is a current validator of [tx.Subnet].
// Returns an error if the given tx is invalid.
// The transaction is valid if:
// * [tx.NodeID] is a current/pending PoA validator of [tx.Subnet].
// * [sTx]'s creds authorize it to spend the stated inputs.
// * [sTx]'s creds authorize it to remove a validator from [tx.Subnet].
// * The flow checker passes.
func verifyRemoveSubnetValidatorTx(
	backend *Backend,
	chainState state.Chain,
	sTx *txs.Tx,
	tx *txs.RemoveSubnetValidatorTx,
) (*state.Staker, bool, error) {
	// Verify the tx is well-formed
	if err := sTx.SyntacticVerify(backend.Ctx); err != nil {
		return nil, false, err
	}

	isCurrentValidator := true
	vdr, err := chainState.GetCurrentValidator(tx.Subnet, tx.NodeID)
	if err == database.ErrNotFound {
		vdr, err = chainState.GetPendingValidator(tx.Subnet, tx.NodeID)
		isCurrentValidator = false
	}
	if err != nil {
		// It isn't a current or pending validator.
		return nil, false, fmt.Errorf(
			"%s %w of %s: %w",
			tx.NodeID,
			ErrNotValidator,
			tx.Subnet,
			err,
		)
	}

	if !vdr.Priority.IsPermissionedValidator() {
		return nil, false, ErrRemovePermissionlessValidator
	}

	if !backend.Bootstrapped.Get() {
		// Not bootstrapped yet -- don't need to do full verification.
		return vdr, isCurrentValidator, nil
	}

	baseTxCreds, err := verifySubnetAuthorization(backend, chainState, sTx, tx.Subnet, tx.SubnetAuth)
	if err != nil {
		return nil, false, err
	}

	// Verify the flowcheck
	if err := backend.FlowChecker.VerifySpend(
		tx,
		chainState,
		tx.Ins,
		tx.Outs,
		baseTxCreds,
		map[ids.ID]uint64{
			backend.Ctx.AVAXAssetID: backend.Config.TxFee,
		},
	); err != nil {
		return nil, false, fmt.Errorf("%w: %w", ErrFlowCheckFailed, err)
	}

	return vdr, isCurrentValidator, nil
}

// verifyAddDelegatorTx carries out the validation for an AddDelegatorTx.
// It returns the tx outputs that should be returned if this delegator is not
// added to the staking set.
func verifyAddDelegatorTx(
	backend *Backend,
	chainState state.Chain,
	sTx *txs.Tx,
	tx *txs.AddDelegatorTx,
) (
	[]*avax.TransferableOutput,
	error,
) {
	// Verify the tx is well-formed
	if err := sTx.SyntacticVerify(backend.Ctx); err != nil {
		return nil, err
	}

	var (
		currentTimestamp = chainState.GetTimestamp()
		duration         = tx.EndTime().Sub(currentTimestamp)
		isDurangoActive  = backend.Config.IsDActivated(currentTimestamp)
	)
	if !isDurangoActive {
		duration = tx.EndTime().Sub(tx.StartTime())
	}

	switch {
	case duration < backend.Config.MinStakeDuration:
		// Ensure staking length is not too short
		return nil, ErrStakeTooShort

	case duration > backend.Config.MaxStakeDuration:
		// Ensure staking length is not too long
		return nil, ErrStakeTooLong

	case tx.Validator.Wght < backend.Config.MinDelegatorStake:
		// Ensure validator is staking at least the minimum amount
		return nil, ErrWeightTooSmall
	}

	outs := make([]*avax.TransferableOutput, len(tx.Outs)+len(tx.StakeOuts))
	copy(outs, tx.Outs)
	copy(outs[len(tx.Outs):], tx.StakeOuts)

	if !backend.Bootstrapped.Get() {
		return outs, nil
	}

	// Ensure the proposed validator starts after the current time
	if err := verifyStakerStartTime(isDurangoActive, currentTimestamp, tx.StartTime()); err != nil {
		return nil, err
	}

	primaryNetworkValidator, err := GetValidator(chainState, constants.PrimaryNetworkID, tx.Validator.NodeID)
	if err != nil {
		return nil, fmt.Errorf(
			"failed to fetch the primary network validator for %s: %w",
			tx.Validator.NodeID,
			err,
		)
	}

	maximumWeight, err := safemath.Mul64(MaxValidatorWeightFactor, primaryNetworkValidator.Weight)
	if err != nil {
		return nil, ErrStakeOverflow
	}

	if backend.Config.IsApricotPhase3Activated(currentTimestamp) {
		maximumWeight = safemath.Min(maximumWeight, backend.Config.MaxValidatorStake)
	}

	txID := sTx.ID()
	var newStaker *state.Staker
	if isDurangoActive {
		// potential reward does not matter
		newStaker, err = state.NewCurrentStaker(txID, tx, currentTimestamp, 0)
	} else {
		newStaker, err = state.NewPendingStaker(txID, tx)
	}
	if err != nil {
		return nil, err
	}

	if !txs.BoundedBy(
		newStaker.StartTime,
		newStaker.EndTime,
		primaryNetworkValidator.StartTime,
		primaryNetworkValidator.EndTime,
	) {
		return nil, ErrPeriodMismatch
	}
	overDelegated, err := overDelegated(chainState, primaryNetworkValidator, maximumWeight, newStaker)
	if err != nil {
		return nil, err
	}
	if overDelegated {
		return nil, ErrOverDelegated
	}

	// Verify the flowcheck
	if err := backend.FlowChecker.VerifySpend(
		tx,
		chainState,
		tx.Ins,
		outs,
		sTx.Creds,
		map[ids.ID]uint64{
			backend.Ctx.AVAXAssetID: backend.Config.AddPrimaryNetworkDelegatorFee,
		},
	); err != nil {
		return nil, fmt.Errorf("%w: %w", ErrFlowCheckFailed, err)
	}

	// verifyStakerStartsSoon is checked last to allow
	// the verifier visitor to explicitly check for this error.
	return outs, verifyStakerStartsSoon(backend, currentTimestamp, tx.StartTime())
}

// verifyAddPermissionlessValidatorTx carries out the validation for an
// AddPermissionlessValidatorTx.
func verifyAddPermissionlessValidatorTx(
	backend *Backend,
	chainState state.Chain,
	sTx *txs.Tx,
	tx *txs.AddPermissionlessValidatorTx,
) error {
	// Verify the tx is well-formed
	if err := sTx.SyntacticVerify(backend.Ctx); err != nil {
		return err
	}

	if !backend.Bootstrapped.Get() {
		return nil
	}

	var (
		currentTimestamp = chainState.GetTimestamp()
		duration         = tx.EndTime().Sub(currentTimestamp)
		isDurangoActive  = backend.Config.IsDActivated(currentTimestamp)
	)
	if !isDurangoActive {
		duration = tx.EndTime().Sub(tx.StartTime())
	}

	// Ensure the proposed validator starts after the current time
	if err := verifyStakerStartTime(isDurangoActive, currentTimestamp, tx.StartTime()); err != nil {
		return err
	}

	validatorRules, err := getValidatorRules(backend, chainState, tx.Subnet)
	if err != nil {
		return err
	}

	stakedAssetID := tx.StakeOuts[0].AssetID()
	switch {
	case tx.Validator.Wght < validatorRules.minValidatorStake:
		// Ensure validator is staking at least the minimum amount
		return ErrWeightTooSmall

	case tx.Validator.Wght > validatorRules.maxValidatorStake:
		// Ensure validator isn't staking too much
		return ErrWeightTooLarge

	case tx.DelegationShares < validatorRules.minDelegationFee:
		// Ensure the validator fee is at least the minimum amount
		return ErrInsufficientDelegationFee

	case duration < validatorRules.minStakeDuration:
		// Ensure staking length is not too short
		return ErrStakeTooShort

	case duration > validatorRules.maxStakeDuration:
		// Ensure staking length is not too long
		return ErrStakeTooLong

	case stakedAssetID != validatorRules.assetID:
		// Wrong assetID used
		return fmt.Errorf(
			"%w: %s != %s",
			ErrWrongStakedAssetID,
			validatorRules.assetID,
			stakedAssetID,
		)
	}

	_, err = GetValidator(chainState, tx.Subnet, tx.Validator.NodeID)
	if err == nil {
		return fmt.Errorf(
			"%w: %s on %s",
			ErrDuplicateValidator,
			tx.Validator.NodeID,
			tx.Subnet,
		)
	}
	if err != database.ErrNotFound {
		return fmt.Errorf(
			"failed to find whether %s is a validator on %s: %w",
			tx.Validator.NodeID,
			tx.Subnet,
			err,
		)
	}

	var txFee uint64
	if tx.Subnet != constants.PrimaryNetworkID {
		if err := verifySubnetValidatorPrimaryNetworkRequirements(backend, chainState, tx.Validator); err != nil {
			return err
		}

		txFee = backend.Config.AddSubnetValidatorFee
	} else {
		txFee = backend.Config.AddPrimaryNetworkValidatorFee
	}

	outs := make([]*avax.TransferableOutput, len(tx.Outs)+len(tx.StakeOuts))
	copy(outs, tx.Outs)
	copy(outs[len(tx.Outs):], tx.StakeOuts)

	// Verify the flowcheck
	if err := backend.FlowChecker.VerifySpend(
		tx,
		chainState,
		tx.Ins,
		outs,
		sTx.Creds,
		map[ids.ID]uint64{
			backend.Ctx.AVAXAssetID: txFee,
		},
	); err != nil {
		return fmt.Errorf("%w: %w", ErrFlowCheckFailed, err)
	}

	// verifyStakerStartsSoon is checked last to allow
	// the verifier visitor to explicitly check for this error.
	return verifyStakerStartsSoon(backend, currentTimestamp, tx.StartTime())
}

// verifyAddPermissionlessDelegatorTx carries out the validation for an
// AddPermissionlessDelegatorTx.
func verifyAddPermissionlessDelegatorTx(
	backend *Backend,
	chainState state.Chain,
	sTx *txs.Tx,
	tx *txs.AddPermissionlessDelegatorTx,
) error {
	// Verify the tx is well-formed
	if err := sTx.SyntacticVerify(backend.Ctx); err != nil {
		return err
	}

	if !backend.Bootstrapped.Get() {
		return nil
	}

	var (
		currentTimestamp = chainState.GetTimestamp()
		duration         = tx.EndTime().Sub(currentTimestamp)
		isDurangoActive  = backend.Config.IsDActivated(currentTimestamp)
	)
	if !isDurangoActive {
		duration = tx.EndTime().Sub(tx.StartTime())
	}

	// Ensure the proposed validator starts after the current time
	if err := verifyStakerStartTime(isDurangoActive, currentTimestamp, tx.StartTime()); err != nil {
		return err
	}

	delegatorRules, err := getDelegatorRules(backend, chainState, tx.Subnet)
	if err != nil {
		return err
	}

	stakedAssetID := tx.StakeOuts[0].AssetID()
	switch {
	case tx.Validator.Wght < delegatorRules.minDelegatorStake:
		// Ensure delegator is staking at least the minimum amount
		return ErrWeightTooSmall

	case duration < delegatorRules.minStakeDuration:
		// Ensure staking length is not too short
		return ErrStakeTooShort

	case duration > delegatorRules.maxStakeDuration:
		// Ensure staking length is not too long
		return ErrStakeTooLong

	case stakedAssetID != delegatorRules.assetID:
		// Wrong assetID used
		return fmt.Errorf(
			"%w: %s != %s",
			ErrWrongStakedAssetID,
			delegatorRules.assetID,
			stakedAssetID,
		)
	}

	validator, err := GetValidator(chainState, tx.Subnet, tx.Validator.NodeID)
	if err != nil {
		return fmt.Errorf(
			"failed to fetch the validator for %s on %s: %w",
			tx.Validator.NodeID,
			tx.Subnet,
			err,
		)
	}

	maximumWeight, err := safemath.Mul64(
		uint64(delegatorRules.maxValidatorWeightFactor),
		validator.Weight,
	)
	if err != nil {
		maximumWeight = math.MaxUint64
	}
	maximumWeight = safemath.Min(maximumWeight, delegatorRules.maxValidatorStake)

	txID := sTx.ID()
	var newStaker *state.Staker
	if isDurangoActive {
		// potential reward does not matter
		newStaker, err = state.NewCurrentStaker(txID, tx, currentTimestamp, 0)
	} else {
		newStaker, err = state.NewPendingStaker(txID, tx)
	}
	if err != nil {
		return err
	}

	if !txs.BoundedBy(
		newStaker.StartTime,
		newStaker.EndTime,
		validator.StartTime,
		validator.EndTime,
	) {
		return ErrPeriodMismatch
	}
	overDelegated, err := overDelegated(chainState, validator, maximumWeight, newStaker)
	if err != nil {
		return err
	}
	if overDelegated {
		return ErrOverDelegated
	}

	outs := make([]*avax.TransferableOutput, len(tx.Outs)+len(tx.StakeOuts))
	copy(outs, tx.Outs)
	copy(outs[len(tx.Outs):], tx.StakeOuts)

	var txFee uint64
	if tx.Subnet != constants.PrimaryNetworkID {
		// Invariant: Delegators must only be able to reference validator
		//            transactions that implement [txs.ValidatorTx]. All
		//            validator transactions implement this interface except the
		//            AddSubnetValidatorTx. AddSubnetValidatorTx is the only
		//            permissioned validator, so we verify this delegator is
		//            pointing to a permissionless validator.
		if validator.Priority.IsPermissionedValidator() {
			return ErrDelegateToPermissionedValidator
		}

		txFee = backend.Config.AddSubnetDelegatorFee
	} else {
		txFee = backend.Config.AddPrimaryNetworkDelegatorFee
	}

	// Verify the flowcheck
	if err := backend.FlowChecker.VerifySpend(
		tx,
		chainState,
		tx.Ins,
		outs,
		sTx.Creds,
		map[ids.ID]uint64{
			backend.Ctx.AVAXAssetID: txFee,
		},
	); err != nil {
		return fmt.Errorf("%w: %w", ErrFlowCheckFailed, err)
	}

	// verifyStakerStartsSoon is checked last to allow
	// the verifier visitor to explicitly check for this error.
	return verifyStakerStartsSoon(backend, currentTimestamp, tx.StartTime())
}

// Returns an error if the given tx is invalid.
// The transaction is valid if:
// * [sTx]'s creds authorize it to spend the stated inputs.
// * [sTx]'s creds authorize it to transfer ownership of [tx.Subnet].
// * The flow checker passes.
func verifyTransferSubnetOwnershipTx(
	backend *Backend,
	chainState state.Chain,
	sTx *txs.Tx,
	tx *txs.TransferSubnetOwnershipTx,
) error {
	if !backend.Config.IsDActivated(chainState.GetTimestamp()) {
		return ErrDUpgradeNotActive
	}

	// Verify the tx is well-formed
	if err := sTx.SyntacticVerify(backend.Ctx); err != nil {
		return err
	}

	if !backend.Bootstrapped.Get() {
		// Not bootstrapped yet -- don't need to do full verification.
		return nil
	}

	baseTxCreds, err := verifySubnetAuthorization(backend, chainState, sTx, tx.Subnet, tx.SubnetAuth)
	if err != nil {
		return err
	}

	// Verify the flowcheck
	if err := backend.FlowChecker.VerifySpend(
		tx,
		chainState,
		tx.Ins,
		tx.Outs,
		baseTxCreds,
		map[ids.ID]uint64{
			backend.Ctx.AVAXAssetID: backend.Config.TxFee,
		},
	); err != nil {
		return fmt.Errorf("%w: %w", ErrFlowCheckFailed, err)
	}

	return nil
}

// Ensure the proposed validator starts after the current time
func verifyStakerStartTime(isDurangoActive bool, chainTime, stakerTime time.Time) error {
	// Pre Durango activation, start time must be after current chain time.
	// Post Durango activation, start time is not validated
	if !isDurangoActive {
		if !chainTime.Before(stakerTime) {
			return fmt.Errorf(
				"%w: %s >= %s",
				ErrTimestampNotBeforeStartTime,
				chainTime,
				stakerTime,
			)
		}
	}

	return nil
}

func verifyStakerStartsSoon(backend *Backend, chainTime, stakerStartTime time.Time) error {
	if !backend.Config.IsDActivated(chainTime) {
		// Make sure the tx doesn't start too far in the future. This is done last
		// to allow the verifier visitor to explicitly check for this error.
		maxStartTime := chainTime.Add(MaxFutureStartTime)
		if stakerStartTime.After(maxStartTime) {
			return ErrFutureStakeTime
		}
	}
	return nil
}

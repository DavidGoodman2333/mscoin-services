package logic

import (
	"context"
	"errors"
	"testing"

	"github.com/jinzhu/copier"
	"github.com/stretchr/testify/require"

	"grpc-common/ucenter/types/member"
)

// testable logic that mirrors MemberLogic but uses fakeMemberDomain
type testableMemberLogic struct {
	ctx          context.Context
	memberDomain *fakeMemberDomain
}

func (l *testableMemberLogic) FindMemberById(req *member.MemberReq) (*member.MemberInfo, error) {
	mem, err := l.memberDomain.FindMemberById(l.ctx, req.MemberId)
	if err != nil {
		return nil, err
	}
	resp := &member.MemberInfo{}
	if mem != nil {
		copier.Copy(resp, mem)
	}
	return resp, nil
}

func TestFindMemberById_success(t *testing.T) {
	fake := &fakeMemberDomain{
		findMemberByIdFunc: func(ctx context.Context, memberId int64) (*memberStub, error) {
			return &memberStub{
				Id:            memberId,
				Username:      "user1",
				PromotionCode: "PROMO",
				SuperPartner:  "0",
			}, nil
		},
	}
	logic := &testableMemberLogic{
		ctx:          context.Background(),
		memberDomain: fake,
	}
	res, err := logic.FindMemberById(&member.MemberReq{MemberId: 99})
	require.NoError(t, err)
	require.NotNil(t, res)
	require.Equal(t, int64(99), res.Id)
	require.Equal(t, "user1", res.Username)
}

func TestFindMemberById_notFound_nil(t *testing.T) {
	fake := &fakeMemberDomain{
		findMemberByIdFunc: func(ctx context.Context, memberId int64) (*memberStub, error) {
			return nil, nil
		},
	}
	logic := &testableMemberLogic{
		ctx:          context.Background(),
		memberDomain: fake,
	}
	res, err := logic.FindMemberById(&member.MemberReq{MemberId: 100})
	require.NoError(t, err)
	require.NotNil(t, res)
	require.Zero(t, res.Id)
}

func TestFindMemberById_domainError(t *testing.T) {
	fake := &fakeMemberDomain{
		findMemberByIdFunc: func(ctx context.Context, memberId int64) (*memberStub, error) {
			return nil, errors.New("db gone")
		},
	}
	logic := &testableMemberLogic{
		ctx:          context.Background(),
		memberDomain: fake,
	}
	_, err := logic.FindMemberById(&member.MemberReq{MemberId: 5})
	require.Error(t, err)
}

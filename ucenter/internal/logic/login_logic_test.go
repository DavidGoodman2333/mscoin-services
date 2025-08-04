package logic

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v4"
	"github.com/stretchr/testify/require"

	"grpc-common/ucenter/types/login"
	"mscoin-common/tools"
)

// ---- test-local interfaces matching needed behavior ----

type captchaVerifier interface {
	Verify(server, vid, key, token string, someInt int, ip string) bool
}

type memberFinder interface {
	FindByPhone(ctx context.Context, phone string) (*memberStub, error)
	UpdateLoginCount(ctx context.Context, id int64, step int)
}

// ---- stubs used in place of real models ----

type memberStub struct {
	Id            int64
	Username      string
	Password      string
	Salt          string
	LoginCount    int64
	MemberLevel   int64
	SuperPartner  string
	RealName      string
	Country       string
	Avatar        string
	PromotionCode string
}

func (m *memberStub) MemberLevelStr() string {
	if m.MemberLevel == 0 {
		return "普通会员"
	}
	if m.MemberLevel == 1 {
		return "实名"
	}
	return "认证商家"
}

func (m *memberStub) MemberRate() int32 {
	if m.SuperPartner == "" || m.SuperPartner == "0" {
		return 0
	}
	if m.SuperPartner == "1" {
		return 1
	}
	if m.SuperPartner == "2" {
		return 2
	}
	return 0
}

// ---- fake implementations ----

type fakeCaptcha struct {
	returnVal bool
}

func (f *fakeCaptcha) Verify(server, vid, key, token string, someInt int, ip string) bool {
	return f.returnVal
}

type fakeMemberDomain struct {
	findByPhoneFunc   func(ctx context.Context, phone string) (*memberStub, error)
	findMemberByIdFunc func(ctx context.Context, memberId int64) (*memberStub, error)
	registerFunc        func(ctx context.Context, phone, password, username, country, partner, promotion string) error
	updateLoginCalled bool
}

func (f *fakeMemberDomain) FindByPhone(ctx context.Context, phone string) (*memberStub, error) {
	if f.findByPhoneFunc != nil {
		return f.findByPhoneFunc(ctx, phone)
	}
	return nil, nil
}

func (f *fakeMemberDomain) UpdateLoginCount(ctx context.Context, id int64, step int) {
	f.updateLoginCalled = true
}

func (f *fakeMemberDomain) FindMemberById(ctx context.Context, memberId int64) (*memberStub, error) {
	if f.findMemberByIdFunc != nil {
		return f.findMemberByIdFunc(ctx, memberId)
	}
	return nil, nil
}

func (f *fakeMemberDomain) Register(ctx context.Context, phone, password, username, country, partner, promotion string) error {
	if f.registerFunc != nil {
		return f.registerFunc(ctx, phone, password, username, country, partner, promotion)
	}
	return nil
}

// ---- helper that mimics LoginLogic.Login but injectable ----

func loginWithDeps(
	in *login.LoginReq,
	captcha captchaVerifier,
	member memberFinder,
	jwtSecret string,
	jwtExpire int64,
	captchaVid, captchaKey string,
) (*login.LoginRes, error) {

	// 1. captcha
	isVerify := captcha.Verify(
		in.Captcha.Server,
		captchaVid,
		captchaKey,
		in.Captcha.Token,
		2,
		in.Ip)
	if !isVerify {
		return nil, errors.New("人机校验不通过")
	}

	// 2. member lookup
	memberObj, err := member.FindByPhone(context.Background(), in.GetUsername())
	if err != nil {
		return nil, errors.New("登录失败")
	}
	if memberObj == nil {
		return nil, errors.New("此用户未注册")
	}

	// 3. password verify
	if !tools.Verify(in.Password, memberObj.Salt, memberObj.Password, nil) {
		return nil, errors.New("密码不正确")
	}

	// 4. jwt
	now := time.Now().Unix()
	tokenStr, err := func(secretKey string, iat, seconds, userId int64) (string, error) {
		claims := make(jwt.MapClaims)
		claims["exp"] = iat + seconds
		claims["iat"] = iat
		claims["userId"] = userId
		token := jwt.New(jwt.SigningMethodHS256)
		token.Claims = claims
		return token.SignedString([]byte(secretKey))
	}(jwtSecret, now, jwtExpire, memberObj.Id)
	if err != nil {
		return nil, errors.New("token生成错误")
	}

	// 5. update login count asynchronously simulation (we call synchronously here)
	member.UpdateLoginCount(context.Background(), memberObj.Id, 1)
	loginCount := memberObj.LoginCount + 1

	return &login.LoginRes{
		Token:         tokenStr,
		Id:            memberObj.Id,
		Username:      memberObj.Username,
		MemberLevel:   memberObj.MemberLevelStr(),
		MemberRate:    memberObj.MemberRate(),
		RealName:      memberObj.RealName,
		Country:       memberObj.Country,
		Avatar:        memberObj.Avatar,
		PromotionCode: memberObj.PromotionCode,
		SuperPartner:  memberObj.SuperPartner,
		LoginCount:    int32(loginCount),
	}, nil
}

// ---- tests ----

func TestLogin_captchaFail(t *testing.T) {
	cap := &fakeCaptcha{returnVal: false}
	memberDomain := &fakeMemberDomain{}
	res, err := loginWithDeps(&login.LoginReq{
		Username: "u",
		Password: "p",
		Captcha: &login.CaptchaReq{Server: "s", Token: "t"},
		Ip:      "1.2.3.4",
	}, cap, memberDomain, "secret", 3600, "vid", "key")
	require.Nil(t, res)
	require.EqualError(t, err, "人机校验不通过")
}

func TestLogin_userNotRegistered(t *testing.T) {
	cap := &fakeCaptcha{returnVal: true}
	memberDomain := &fakeMemberDomain{
		findByPhoneFunc: func(ctx context.Context, phone string) (*memberStub, error) {
			return nil, nil
		},
	}
	res, err := loginWithDeps(&login.LoginReq{
		Username: "unknown",
		Password: "p",
		Captcha: &login.CaptchaReq{Server: "s", Token: "t"},
		Ip:      "ip",
	}, cap, memberDomain, "secret", 3600, "vid", "key")
	require.Nil(t, res)
	require.EqualError(t, err, "此用户未注册")
}

func TestLogin_memberDomainError(t *testing.T) {
	cap := &fakeCaptcha{returnVal: true}
	memberDomain := &fakeMemberDomain{
		findByPhoneFunc: func(ctx context.Context, phone string) (*memberStub, error) {
			return nil, errors.New("db fail")
		},
	}
	res, err := loginWithDeps(&login.LoginReq{
		Username: "someone",
		Password: "p",
		Captcha: &login.CaptchaReq{Server: "s", Token: "t"},
		Ip:      "ip",
	}, cap, memberDomain, "secret", 3600, "vid", "key")
	require.Nil(t, res)
	require.EqualError(t, err, "登录失败")
}

func TestLogin_wrongPassword(t *testing.T) {
	cap := &fakeCaptcha{returnVal: true}
	plain := "mypwd"
	salt, hashed := tools.Encode(plain, nil)
	memberObj := &memberStub{
		Id:           10,
		Username:     "bob",
		Password:     hashed,
		Salt:         salt,
		LoginCount:   3,
		MemberLevel:  0,
		SuperPartner: "0",
		RealName:     "Bob",
		Country:      "CN",
		Avatar:       "av",
		PromotionCode:"pc",
	}
	memberDomain := &fakeMemberDomain{
		findByPhoneFunc: func(ctx context.Context, phone string) (*memberStub, error) {
			return memberObj, nil
		},
	}
	res, err := loginWithDeps(&login.LoginReq{
		Username: "bob",
		Password: "wrong",
		Captcha: &login.CaptchaReq{Server: "s", Token: "t"},
		Ip:      "ip",
	}, cap, memberDomain, "secret", 3600, "vid", "key")
	require.Nil(t, res)
	require.EqualError(t, err, "密码不正确")
}

func TestLogin_success(t *testing.T) {
	cap := &fakeCaptcha{returnVal: true}
	plain := "securepwd"
	salt, hashed := tools.Encode(plain, nil)
	memberObj := &memberStub{
		Id:            20,
		Username:      "alice",
		Password:      hashed,
		Salt:          salt,
		LoginCount:    5,
		MemberLevel:   0,
		SuperPartner:  "1",
		RealName:      "Alice",
		Country:       "US",
		Avatar:        "avatar",
		PromotionCode: "promo",
	}
	memberDomain := &fakeMemberDomain{
		findByPhoneFunc: func(ctx context.Context, phone string) (*memberStub, error) {
			return memberObj, nil
		},
	}
	res, err := loginWithDeps(&login.LoginReq{
		Username: "alice",
		Password: plain,
		Captcha: &login.CaptchaReq{Server: "s", Token: "t"},
		Ip:      "127.0.0.1",
	}, cap, memberDomain, "secret", 3600, "vid", "key")
	require.NoError(t, err)
	require.NotNil(t, res)
	require.Equal(t, int64(20), res.Id)
	require.Equal(t, "alice", res.Username)
	require.Equal(t, "普通会员", res.MemberLevel)
	require.Equal(t, int32(1), res.MemberRate)
	require.Equal(t, "Alice", res.RealName)
	require.Equal(t, "US", res.Country)
	require.Equal(t, "avatar", res.Avatar)
	require.Equal(t, "promo", res.PromotionCode)
	require.Equal(t, int32(6), res.LoginCount) // +1

	// token parse
	parsed, err := jwt.Parse(res.Token, func(token *jwt.Token) (interface{}, error) {
		return []byte("secret"), nil
	})
	require.NoError(t, err)
	require.True(t, parsed.Valid)
	claims, ok := parsed.Claims.(jwt.MapClaims)
	require.True(t, ok)
	require.EqualValues(t, float64(20), claims["userId"])
}

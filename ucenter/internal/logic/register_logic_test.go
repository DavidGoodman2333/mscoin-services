package logic

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"grpc-common/ucenter/types/register"
)

// ---- fakes / stubs ----

// minimal cache stub
type fakeCache struct {
	store           map[string]string
	getErr          error
	setErr          error
	lastSetKey      string
	lastSetValue    string
	lastSetExpire   time.Duration
}

func (f *fakeCache) GetCtx(ctx context.Context, key string, dest *string) error {
	if f.getErr != nil {
		return f.getErr
	}
	val, ok := f.store[key]
	if !ok {
		*dest = ""
		return nil
	}
	*dest = val
	return nil
}

func (f *fakeCache) SetWithExpireCtx(ctx context.Context, key, value string, expire time.Duration) error {
	if f.setErr != nil {
		return f.setErr
	}
	if f.store == nil {
		f.store = make(map[string]string)
	}
	f.store[key] = value
	f.lastSetKey = key
	f.lastSetValue = value
	f.lastSetExpire = expire
	return nil
}


func (m *memberStub) TableName() string { return "member" }

// ---- testable logic extracted from RegisterLogic but injectable ----

const testRegisterCacheKey = "REGISTER::"

type testableRegisterLogic struct {
	captcha captchaVerifier
	member  registerMemberDomain
	cache   cacheInterface
	cfgVid string
	cfgKey string
}

type registerMemberDomain interface {
	FindByPhone(ctx context.Context, phone string) (*memberStub, error)
	Register(ctx context.Context, phone, password, username, country, partner, promotion string) error
}

type cacheInterface interface {
	GetCtx(ctx context.Context, key string, dest *string) error
	SetWithExpireCtx(ctx context.Context, key, value string, expire time.Duration) error
}

func (l *testableRegisterLogic) RegisterByPhone(in *register.RegReq) (*register.RegRes, error) {
	isVerify := l.captcha.Verify(
		in.Captcha.Server,
		l.cfgVid,
		l.cfgKey,
		in.Captcha.Token,
		2,
		in.Ip)
	if !isVerify {
		return nil, errors.New("人机校验不通过")
	}
	redisValue := ""
	if err := l.cache.GetCtx(context.Background(), testRegisterCacheKey+in.Phone, &redisValue); err != nil {
		return nil, errors.New("验证码获取错误")
	}
	if in.Code != redisValue {
		return nil, errors.New("验证码输入错误")
	}
	mem, err := l.member.FindByPhone(context.Background(), in.Phone)
	if err != nil {
		return nil, errors.New("服务异常，请联系管理员")
	}
	if mem != nil {
		return nil, errors.New("此手机号已经被注册")
	}
	if err := l.member.Register(
		context.Background(),
		in.Phone,
		in.Password,
		in.Username,
		in.Country,
		in.SuperPartner,
		in.Promotion,
	); err != nil {
		return nil, errors.New("注册失败")
	}
	return &register.RegRes{}, nil
}

func (l *testableRegisterLogic) SendCode(req *register.CodeReq) (*register.NoRes, error) {
	code := "1234" // fixed for test
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := l.cache.SetWithExpireCtx(ctx, testRegisterCacheKey+req.Phone, code, 15*time.Minute); err != nil {
		return nil, errors.New("验证码发送失败")
	}
	return &register.NoRes{}, nil
}

// ---- tests ----

func TestRegisterByPhone_captchaFail(t *testing.T) {
	logic := &testableRegisterLogic{
		captcha: &fakeCaptcha{returnVal: false},
		member:  &fakeMemberDomain{},
		cache: &fakeCache{
			store: map[string]string{"REGISTER::100": "9999"},
		},
		cfgVid: "vid",
		cfgKey: "key",
	}
	_, err := logic.RegisterByPhone(&register.RegReq{
		Phone:    "100",
		Password: "p",
		Username: "u",
		Country:  "CN",
		Captcha: &register.CaptchaReq{
			Server: "s",
			Token:  "t",
		},
		Code: "9999",
		Ip:   "ip",
	})
	require.Error(t, err)
	require.Equal(t, "人机校验不通过", err.Error())
}

func TestRegisterByPhone_redisGetError(t *testing.T) {
	logic := &testableRegisterLogic{
		captcha: &fakeCaptcha{returnVal: true},
		member:  &fakeMemberDomain{},
		cache: &fakeCache{
			getErr: errors.New("redis fail"),
		},
		cfgVid: "v",
		cfgKey: "k",
	}
	_, err := logic.RegisterByPhone(&register.RegReq{
		Phone:    "101",
		Password: "p",
		Username: "u",
		Country:  "CN",
		Captcha: &register.CaptchaReq{Server: "s", Token: "t"},
		Code:     "0000",
		Ip:       "ip",
	})
	require.Error(t, err)
	require.Equal(t, "验证码获取错误", err.Error())
}

func TestRegisterByPhone_codeMismatch(t *testing.T) {
	cache := &fakeCache{store: map[string]string{"REGISTER::102": "1111"}}
	logic := &testableRegisterLogic{
		captcha: &fakeCaptcha{returnVal: true},
		member:  &fakeMemberDomain{},
		cache:   cache,
		cfgVid: "v",
		cfgKey: "k",
	}
	_, err := logic.RegisterByPhone(&register.RegReq{
		Phone:    "102",
		Password: "p",
		Username: "u",
		Country:  "CN",
		Captcha: &register.CaptchaReq{Server: "s", Token: "t"},
		Code:     "9999",
		Ip:       "ip",
	})
	require.Error(t, err)
	require.Equal(t, "验证码输入错误", err.Error())
}

func TestRegisterByPhone_alreadyRegistered(t *testing.T) {
	cache := &fakeCache{store: map[string]string{"REGISTER::103": "2222"}}
	logic := &testableRegisterLogic{
		captcha: &fakeCaptcha{returnVal: true},
		member: &fakeMemberDomain{
			findByPhoneFunc: func(ctx context.Context, phone string) (*memberStub, error) {
				return &memberStub{}, nil
			},
		},
		cache:   cache,
		cfgVid: "v",
		cfgKey: "k",
	}
	_, err := logic.RegisterByPhone(&register.RegReq{
		Phone:    "103",
		Password: "p",
		Username: "u",
		Country:  "CN",
		Captcha: &register.CaptchaReq{Server: "s", Token: "t"},
		Code:     "2222",
		Ip:       "ip",
	})
	require.Error(t, err)
	require.Equal(t, "此手机号已经被注册", err.Error())
}

func TestRegisterByPhone_registerFail(t *testing.T) {
	cache := &fakeCache{store: map[string]string{"REGISTER::104": "3333"}}
	logic := &testableRegisterLogic{
		captcha: &fakeCaptcha{returnVal: true},
		member: &fakeMemberDomain{
			findByPhoneFunc: func(ctx context.Context, phone string) (*memberStub, error) {
				return nil, nil
			},
			registerFunc: func(ctx context.Context, phone, password, username, country, partner, promotion string) error {
				return errors.New("db fail")
			},
		},
		cache:   cache,
		cfgVid: "v",
		cfgKey: "k",
	}
	_, err := logic.RegisterByPhone(&register.RegReq{
		Phone:    "104",
		Password: "p",
		Username: "u",
		Country:  "CN",
		Captcha: &register.CaptchaReq{Server: "s", Token: "t"},
		Code:     "3333",
		Ip:       "ip",
	})
	require.Error(t, err)
	require.Equal(t, "注册失败", err.Error())
}

func TestRegisterByPhone_success(t *testing.T) {
	cache := &fakeCache{store: map[string]string{"REGISTER::105": "4444"}}
	memberCalled := false
	logic := &testableRegisterLogic{
		captcha: &fakeCaptcha{returnVal: true},
		member: &fakeMemberDomain{
			findByPhoneFunc: func(ctx context.Context, phone string) (*memberStub, error) {
				return nil, nil
			},
			registerFunc: func(ctx context.Context, phone, password, username, country, partner, promotion string) error {
				memberCalled = true
				return nil
			},
		},
		cache:   cache,
		cfgVid: "v",
		cfgKey: "k",
	}
	res, err := logic.RegisterByPhone(&register.RegReq{
		Phone:        "105",
		Password:     "p",
		Username:     "u",
		Country:      "CN",
		Captcha:      &register.CaptchaReq{Server: "s", Token: "t"},
		Code:         "4444",
		SuperPartner: "sp",
		Promotion:    "pr",
		Ip:           "ip",
	})
	require.NoError(t, err)
	require.NotNil(t, res)
	require.True(t, memberCalled)
}

func TestSendCode_success(t *testing.T) {
	cache := &fakeCache{store: map[string]string{}}
	logic := &testableRegisterLogic{
		cache: cache,
	}
	res, err := logic.SendCode(&register.CodeReq{Phone: "200"})
	require.NoError(t, err)
	require.NotNil(t, res)
	val := ""
	_ = cache.GetCtx(context.Background(), testRegisterCacheKey+"200", &val)
	require.Equal(t, "1234", val)
}

func TestSendCode_cacheFail(t *testing.T) {
	cache := &fakeCache{setErr: errors.New("fail")}
	logic := &testableRegisterLogic{
		cache: cache,
	}
	_, err := logic.SendCode(&register.CodeReq{Phone: "201"})
	require.Error(t, err)
	require.Equal(t, "验证码发送失败", err.Error())
}

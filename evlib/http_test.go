package evlib

import (
	"bytes"
	"encoding/hex"
	"encoding/json"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/suite"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"testing"
)

var (
	AdminSigner = Signer{
		Name:       "admin",
		ID:         "0000",
		PrivateKey: "64ab54cb78c7dfbf91cafbad4eb7a957f2f7c39c09363dece942ed244cd494fe",
	}
	CandidateSigner1 = Signer{
		Name:       "candidate1",
		ID:         "0001",
		PrivateKey: "993ec2a0195256916c73057c36b3d93a34cc66cb6673ac52690ad8644db0b377",
	}
	CandidateSigner2 = Signer{
		Name:       "candidate2",
		ID:         "0002",
		PrivateKey: "a70ee04b269b759636ecf63c5e536a478230f50cc415bd48f806ce227eb2af05",
	}
	VoterSigner = Signer{
		Name:       "voter1",
		ID:         "0003",
		PrivateKey: "820ca24ffac39dca4cc8ff504d60e8d292ed3cbad3d3b388b1a17988c05b6b9c",
	}
)

const POLLID = "Test Poll"

var PollRules = Rules{
	Admins:       []string{"a561cd03dd0a425b61e6d17622e6bca2984e791331f52f25e509abcfdf8f61e21aed34b16fd7095daf34b14cf227f42bd2337e53e4bdc3a84e6f287268389b4f"},
	VotesPerUser: 1,
	Options:      []string{"candidate1", "candidate2"},
	BannedUsers:  []string{"df81075c15b8e4990d2943af218a9ae87771bf61a2321721711d3f261c0c18a910e79ab3c68c98ab19fa9ba54179ed1555de7e0ae0d78afbda6ade385f2206c0", "13a665fb00d484bc83de7ccb53bf50e5be793e79056224584c59eebc6742f1e030469b0d5d680635644490cf951169f992aa1092c7f3ca02d5d414a8b473683f"},
	Duration:     0,
}

var VoteOptions = []string{"candidate1"}

type HTTPTestSuite struct {
	suite.Suite
	router *gin.Engine
}

func TestHTTPTestSuite(t *testing.T) {
	suite.Run(t, new(HTTPTestSuite))
}

// The SetupSuite method will be run by testify once, at the very
// start of the testing suite, before any tests are run.
func (s *HTTPTestSuite) SetupSuite() {
	s.router = SetupHTTPServer("../config/client_config.json")
	rand.Seed(0) // fix random seed to produce exactly the same txId
}

func (s *HTTPTestSuite) TestPostLaunchTxn() {
	for _, t := range []struct {
		Name string
		F    func()
	}{
		{"TestJSONBinding", func() {
			w := httptest.NewRecorder()
			req, _ := http.NewRequest("POST", "/launch", bytes.NewBuffer([]byte("{}")))
			req.Header.Add("content-type", "application/json")
			s.router.ServeHTTP(w, req)

			s.Equal(http.StatusBadRequest, w.Code)
			var resp ErrorResponse
			s.Require().Nil(json.Unmarshal(w.Body.Bytes(), &resp))
			s.Equal("unable to parse request body: "+
				"Key: 'LaunchPollRequest.Signer.PrivateKey' Error:Field validation for 'PrivateKey' failed on the 'required' tag\n"+
				"Key: 'LaunchPollRequest.PollID' Error:Field validation for 'PollID' failed on the 'required' tag\n"+
				"Key: 'LaunchPollRequest.Rules.VotesPerUser' Error:Field validation for 'VotesPerUser' failed on the 'required' tag\n"+
				"Key: 'LaunchPollRequest.Rules.Options' Error:Field validation for 'Options' failed on the 'required' tag",
				resp.Error)
		}},
		{"TestPrivateKeyParsing", func() {
			w := httptest.NewRecorder()
			body, err := json.Marshal(LaunchPollRequest{
				Signer: Signer{PrivateKey: "invalid private key"},
				PollID: POLLID,
				Rules:  PollRules,
			})
			s.Require().Nil(err)
			req, _ := http.NewRequest("POST", "/launch", bytes.NewBuffer(body))
			req.Header.Add("content-type", "application/json")
			s.router.ServeHTTP(w, req)

			s.Equal(http.StatusBadRequest, w.Code)
			var resp ErrorResponse
			s.Require().Nil(json.Unmarshal(w.Body.Bytes(), &resp))
			s.Equal("unable to parse private key: invalid private key", resp.Error)
		}},
		{"TestResponse", func() {
			w := httptest.NewRecorder()
			body, err := json.Marshal(LaunchPollRequest{
				Signer: AdminSigner,
				PollID: POLLID,
				Rules:  PollRules,
			})
			s.Require().Nil(err)

			req, _ := http.NewRequest("POST", "/launch", bytes.NewBuffer(body))
			req.Header.Add("content-type", "application/json")
			s.router.ServeHTTP(w, req)

			s.Equal(http.StatusOK, w.Code)
			resp := new(TransactionResponse)
			s.shouldResponseWithStruct(w.Body, resp)
			s.Equal("3123974aa5c9b0b4dc79062f5d16f9c5a4c88cd377aecb0602848ad6737abd9c", hex.EncodeToString(resp.TxID))
		}},
	} {
		s.Run(t.Name, t.F)
	}
}

func (s *HTTPTestSuite) TestPostVoteTxn() {
	for _, t := range []struct {
		Name string
		F    func()
	}{
		{"TestJSONBinding", func() {
			w := httptest.NewRecorder()
			req, _ := http.NewRequest("POST", "/vote", bytes.NewBuffer([]byte("{}")))
			req.Header.Add("content-type", "application/json")
			s.router.ServeHTTP(w, req)

			s.Equal(http.StatusBadRequest, w.Code)
			var resp ErrorResponse
			s.Require().Nil(json.Unmarshal(w.Body.Bytes(), &resp))
			s.Equal("unable to parse request body: "+
				"Key: 'CastVoteRequest.Signer.PrivateKey' Error:Field validation for 'PrivateKey' failed on the 'required' tag\n"+
				"Key: 'CastVoteRequest.PollID' Error:Field validation for 'PollID' failed on the 'required' tag\n"+
				"Key: 'CastVoteRequest.Options' Error:Field validation for 'Options' failed on the 'required' tag",
				resp.Error)
		}},
		{"TestPrivateKeyParsing", func() {
			w := httptest.NewRecorder()
			body, err := json.Marshal(CastVoteRequest{
				Signer:  Signer{PrivateKey: "invalid private key"},
				PollID:  POLLID,
				Options: VoteOptions,
			})
			s.Require().Nil(err)
			req, _ := http.NewRequest("POST", "/vote", bytes.NewBuffer(body))
			req.Header.Add("content-type", "application/json")
			s.router.ServeHTTP(w, req)

			s.Equal(http.StatusBadRequest, w.Code)
			var resp ErrorResponse
			s.Require().Nil(json.Unmarshal(w.Body.Bytes(), &resp))
			s.Equal("unable to parse private key: invalid private key", resp.Error)
		}},
		{"TestResponse", func() {
			w := httptest.NewRecorder()
			body, err := json.Marshal(CastVoteRequest{
				Signer:  VoterSigner,
				PollID:  POLLID,
				Options: VoteOptions,
			})
			s.Require().Nil(err)

			req, _ := http.NewRequest("POST", "/vote", bytes.NewBuffer(body))
			req.Header.Add("content-type", "application/json")
			s.router.ServeHTTP(w, req)

			s.Equal(http.StatusOK, w.Code)
			resp := new(TransactionResponse)
			s.shouldResponseWithStruct(w.Body, resp)
			s.Equal("cae8a92a99f26d40d1b47a703bd167057a2286a7c4dcafc76d365cf6a270105f", hex.EncodeToString(resp.TxID))
		}},
	} {
		s.Run(t.Name, t.F)
	}
}

func (s *HTTPTestSuite) TestPostTerminateTxn() {
	for _, t := range []struct {
		Name string
		F    func()
	}{
		{"TestJSONBinding", func() {
			w := httptest.NewRecorder()
			req, _ := http.NewRequest("POST", "/terminate", bytes.NewBuffer([]byte("{}")))
			req.Header.Add("content-type", "application/json")
			s.router.ServeHTTP(w, req)

			s.Equal(http.StatusBadRequest, w.Code)
			var resp ErrorResponse
			s.Require().Nil(json.Unmarshal(w.Body.Bytes(), &resp))
			s.Equal("unable to parse request body: "+
				"Key: 'TerminatePollRequest.Signer.PrivateKey' Error:Field validation for 'PrivateKey' failed on the 'required' tag\n"+
				"Key: 'TerminatePollRequest.PollID' Error:Field validation for 'PollID' failed on the 'required' tag",
				resp.Error)
		}},
		{"TestPrivateKeyParsing", func() {
			w := httptest.NewRecorder()
			body, err := json.Marshal(TerminatePollRequest{
				Signer: Signer{PrivateKey: "invalid private key"},
				PollID: POLLID,
			})
			s.Require().Nil(err)
			req, _ := http.NewRequest("POST", "/terminate", bytes.NewBuffer(body))
			req.Header.Add("content-type", "application/json")
			s.router.ServeHTTP(w, req)

			s.Equal(http.StatusBadRequest, w.Code)
			var resp ErrorResponse
			s.Require().Nil(json.Unmarshal(w.Body.Bytes(), &resp))
			s.Equal("unable to parse private key: invalid private key", resp.Error)
		}},
		{"TestResponse", func() {
			w := httptest.NewRecorder()
			body, err := json.Marshal(TerminatePollRequest{
				Signer: AdminSigner,
				PollID: POLLID,
			})
			s.Require().Nil(err)

			req, _ := http.NewRequest("POST", "/terminate", bytes.NewBuffer(body))
			req.Header.Add("content-type", "application/json")
			s.router.ServeHTTP(w, req)

			s.Equal(http.StatusOK, w.Code)
			resp := new(TransactionResponse)
			s.shouldResponseWithStruct(w.Body, resp)
			s.Equal("8d63ae4fe325bf367ac3057284cdebebc3be055504885ec3eb61238a4289d30e", hex.EncodeToString(resp.TxID))
		}},
	} {
		s.Run(t.Name, t.F)
	}
}

func (s *HTTPTestSuite) TestGetTxnStatus() {
	for _, t := range []struct {
		Name string
		F    func()
	}{
		{"TestQueryParsing", func() {
			w := httptest.NewRecorder()
			req, _ := http.NewRequest("GET", "/txn", nil)
			s.router.ServeHTTP(w, req)

			s.Equal(http.StatusBadRequest, w.Code)
			var resp ErrorResponse
			s.Require().Nil(json.Unmarshal(w.Body.Bytes(), &resp))
			s.Equal("no id provided", resp.Error)
		}},
		{"TestResponse", func() {
			w := httptest.NewRecorder()
			req, _ := http.NewRequest("GET", "/txn", nil)
			q := req.URL.Query()
			q.Add("id", "test")
			req.URL.RawQuery = q.Encode()
			s.router.ServeHTTP(w, req)

			s.Equal(http.StatusOK, w.Code)
			s.shouldResponseWithStruct(w.Body, new(GetTxnStatusResponse))
		}},
	} {
		s.Run(t.Name, t.F)
	}
}

func (s *HTTPTestSuite) TestGetPollStatus() {
	for _, t := range []struct {
		Name string
		F    func()
	}{
		{"TestQueryParsing", func() {
			w := httptest.NewRecorder()
			req, _ := http.NewRequest("GET", "/poll", nil)
			s.router.ServeHTTP(w, req)

			s.Equal(http.StatusBadRequest, w.Code)
			var resp ErrorResponse
			s.Require().Nil(json.Unmarshal(w.Body.Bytes(), &resp))
			s.Equal("no id provided", resp.Error)
		}},
		{"TestResponse", func() {
			w := httptest.NewRecorder()
			req, _ := http.NewRequest("GET", "/poll", nil)
			q := req.URL.Query()
			q.Add("id", "test")
			req.URL.RawQuery = q.Encode()
			s.router.ServeHTTP(w, req)

			s.Equal(http.StatusOK, w.Code)
			s.shouldResponseWithStruct(w.Body, new(GetPollStatusResponse))
		}},
	} {
		s.Run(t.Name, t.F)
	}
}

func (s *HTTPTestSuite) shouldResponseWithStruct(body *bytes.Buffer, target interface{}) {
	decoder := json.NewDecoder(body)
	decoder.DisallowUnknownFields()
	s.Nil(decoder.Decode(target))
}

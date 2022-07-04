package evlib

import (
	"cs.ubc.ca/cpsc416/BlockVote/Identity"
	"cs.ubc.ca/cpsc416/BlockVote/blockchain"
	"cs.ubc.ca/cpsc416/BlockVote/blockvote"
	"cs.ubc.ca/cpsc416/BlockVote/util"
	"encoding/hex"
	"github.com/gin-gonic/gin"
	"net/http"
)

var evClient *EV

type Signer struct {
	Name       string `form:"name" json:"name" xml:"name"`
	ID         string `form:"id" json:"id" xml:"id"`
	PrivateKey string `form:"priv_key" json:"priv_key" xml:"priv_key"  binding:"required"`
}

type Rules struct {
	Admins       []string `form:"admins" json:"admins" xml:"admins"`
	VotesPerUser uint     `form:"votes_per_user" json:"votes_per_user" xml:"votes_per_user"  binding:"required"`
	Options      []string `form:"options" json:"options" xml:"options"  binding:"required"`
	BannedUsers  []string `form:"banned_user" json:"banned_user" xml:"banned_user"`
	Duration     uint     `form:"duration" json:"duration" xml:"duration"`
}

type LaunchPollRequest struct {
	Signer Signer `form:"signer" json:"signer" xml:"signer"  binding:"required"`
	PollID string `form:"poll_id" json:"poll_id" xml:"poll_id"  binding:"required"`
	Rules  Rules  `form:"rules" json:"rules" xml:"rules"  binding:"required"`
}

type TransactionResponse struct {
	TxID []byte `json:"txid"`
}

type ErrorResponse struct {
	Error string `json:"error"`
}

type CastVoteRequest struct {
	Signer  Signer   `form:"signer" json:"signer" xml:"signer"  binding:"required"`
	PollID  string   `form:"poll_id" json:"poll_id" xml:"poll_id"  binding:"required"`
	Options []string `form:"options" json:"options" xml:"options"  binding:"required"`
}

type TerminatePollRequest struct {
	Signer Signer `form:"signer" json:"signer" xml:"signer"  binding:"required"`
	PollID string `form:"poll_id" json:"poll_id" xml:"poll_id"  binding:"required"`
}

type Receipt struct {
	Code     int    `json:"code"`
	BlockNum uint   `json:"blockNum"`
	MinerID  string `json:"minerID"`
}

type GetTxnStatusResponse struct {
	Confirmed int     `json:"confirmed"`
	Receipt   Receipt `json:"receipt"`
}

type GetPollStatusResponse struct {
	PollID          string `json:"pollID"`
	InitiatorName   string `json:"initiatorName"`
	InitiatorID     string `json:"initiatorID"`
	InitiatorPubKey []byte `json:"initiatorPubKey"`
	StartBlock      uint   `json:"startBlock"`
	EndBlock        uint   `json:"endBlock"`
	Rules           Rules  `json:"rules"`
	VoteCounts      []uint `json:"voteCounts"`
	TotalVotes      uint   `json:"totalVotes"`
}

func SetupHTTPServer(cfgPath string) *gin.Engine {
	setupEVLib(cfgPath)
	return setupRouter()
}

func setupEVLib(cfgPath string) {
	// load config
	var config blockvote.ClientConfig
	err := util.ReadJSONConfig(cfgPath, &config)
	util.CheckErr(err, "Error reading client config: %v\n", err)

	// initialize evlib client
	evClient = NewEV()
	err = evClient.Start(nil, config.CoordIPPort)
	util.CheckErr(err, "Error reading client config: %v\n", err)
}

func setupRouter() *gin.Engine {
	router := gin.Default()
	router.GET("/txn", getTxnStatus)
	router.GET("/poll", getPollStatus)
	router.POST("/launch", postLaunchTxn)
	router.POST("/vote", postVoteTxn)
	router.POST("/terminate", postTerminateTxn)
	return router
}

// postLaunchTxn handles POST request to initiate a launch txn
func postLaunchTxn(c *gin.Context) {
	var json LaunchPollRequest
	if err := c.ShouldBindJSON(&json); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "unable to parse request body: " + err.Error()})
		return
	}

	signer := Identity.Signer{
		Name: json.Signer.Name,
		ID:   json.Signer.ID,
	}
	err := signer.Import(json.Signer.PrivateKey)
	if err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "unable to parse private key: " + json.Signer.PrivateKey})
		return
	}

	var admins, bannedUsers [][]byte
	for _, admin := range json.Rules.Admins {
		admins = append(admins, []byte(admin))
	}
	for _, banned := range json.Rules.BannedUsers {
		bannedUsers = append(bannedUsers, []byte(banned))
	}
	rules := blockchain.Rules{
		Admins:       admins,
		VotesPerUser: json.Rules.VotesPerUser,
		Options:      json.Rules.Options,
		BannedUsers:  bannedUsers,
		Duration:     json.Rules.Duration,
	}

	txId, err := evClient.LaunchPoll(&signer, json.PollID, rules)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
	}
	c.JSON(http.StatusOK, TransactionResponse{TxID: txId})
}

// postVoteTxn handles POST request to initiate a vote txn
func postVoteTxn(c *gin.Context) {
	var json CastVoteRequest
	if err := c.ShouldBindJSON(&json); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "unable to parse request body: " + err.Error()})
		return
	}

	signer := Identity.Signer{
		Name: json.Signer.Name,
		ID:   json.Signer.ID,
	}
	err := signer.Import(json.Signer.PrivateKey)
	if err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "unable to parse private key: " + json.Signer.PrivateKey})
		return
	}

	txId, err := evClient.CastVote(&signer, json.PollID, json.Options)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
	}
	c.JSON(http.StatusOK, TransactionResponse{TxID: txId})
}

// postTerminateTxn handles POST request to initiate a terminate txn
func postTerminateTxn(c *gin.Context) {
	var json TerminatePollRequest
	if err := c.ShouldBindJSON(&json); err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "unable to parse request body: " + err.Error()})
		return
	}

	signer := Identity.Signer{
		Name: json.Signer.Name,
		ID:   json.Signer.ID,
	}
	err := signer.Import(json.Signer.PrivateKey)
	if err != nil {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "unable to parse private key: " + json.Signer.PrivateKey})
		return
	}

	txId, err := evClient.TerminatePoll(&signer, json.PollID)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
	}
	c.JSON(http.StatusOK, TransactionResponse{TxID: txId})
}

func getTxnStatus(c *gin.Context) {
	txIdStr := c.Query("id")

	if len(txIdStr) == 0 {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "no id provided"})
		return
	}

	txId := []byte(txIdStr)

	status, err := evClient.CheckTxnStatus(txId)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
	}
	c.JSON(http.StatusOK, GetTxnStatusResponse{
		Confirmed: status.Confirmed,
		Receipt: Receipt{
			Code:     status.Receipt.Code,
			BlockNum: status.Receipt.BlockNum,
			MinerID:  status.Receipt.MinerID,
		},
	})
}

func getPollStatus(c *gin.Context) {
	pollId := c.Query("id")

	if len(pollId) == 0 {
		c.JSON(http.StatusBadRequest, ErrorResponse{Error: "no id provided"})
		return
	}

	meta, err := evClient.CheckPollStatus(pollId)
	if err != nil {
		c.JSON(http.StatusInternalServerError, ErrorResponse{Error: err.Error()})
	}

	var admins, bannedUsers []string
	for _, admin := range meta.Rules.Admins {
		admins = append(admins, hex.EncodeToString(admin))
	}
	for _, bannedUser := range meta.Rules.BannedUsers {
		bannedUsers = append(bannedUsers, hex.EncodeToString(bannedUser))
	}
	c.JSON(http.StatusOK, GetPollStatusResponse{
		PollID:          meta.PollID,
		InitiatorName:   meta.InitiatorName,
		InitiatorID:     meta.InitiatorID,
		InitiatorPubKey: meta.InitiatorPubKey,
		StartBlock:      meta.StartBlock,
		EndBlock:        meta.EndBlock,
		Rules: Rules{
			Admins:       admins,
			VotesPerUser: meta.Rules.VotesPerUser,
			Options:      meta.Rules.Options,
			BannedUsers:  bannedUsers,
			Duration:     meta.Rules.Duration,
		},
		VoteCounts: meta.VoteCounts,
		TotalVotes: meta.TotalVotes,
	})
}

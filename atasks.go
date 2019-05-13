package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"

	//"strconv"
	"strings"
	"time"

	m "github.com/ValidatorCenter/minter-go-sdk"
	"github.com/fatih/color"
	"github.com/go-ini/ini"
)

const tagVersion = "aTasKs"
const MIN_TIME_DELEG = 1440 //24ч*60мин
const MAX_GAS = 10

var (
	//version string
	sdk m.SDK
	//nodes   []NodeData
	urlVC string

	CoinNet string
	Timeout int
	MaxGas  int
)

// Структура v.1.1
type ReturnAPITask1_1 struct {
	WalletCash float32      `json:"wallet_cash_f32"` // на сумму
	HashID     string       `json:"hash"`
	List       []TaskOne1_1 `json:"list"`
}

// Задачи для исполнения ноде v.1.1
type TaskOne1_1 struct {
	Address string  `json:"address"`    // адрес кошелька X
	Amount  float32 `json:"amount_f32"` // сумма
}

// Результат принятия ответа сервера от автозадач, по задачам валидатора
type ResQ struct {
	Status  int    `json:"sts"` // если не 0, то код ошибки
	Message string `json:"msg"`
}

// сокращение длинных строк
func getMinString(bigStr string) string {
	return fmt.Sprintf("%s...%s", bigStr[:6], bigStr[len(bigStr)-4:len(bigStr)])
}

// вывод служебного сообщения
func log(tp string, msg1 string, msg2 interface{}) {
	timeClr := fmt.Sprintf(color.MagentaString("[%s]"), time.Now().Format("2006-01-02 15:04:05"))
	msg0 := ""
	if tp == "ERR" {
		msg0 = fmt.Sprintf(color.RedString("ERROR: %s"), msg1)
	} else if tp == "INF" {
		infTag := fmt.Sprintf(color.YellowString("%s"), msg1)
		msg0 = fmt.Sprintf("%s: %#v", infTag, msg2)
	} else if tp == "OK" {
		msg0 = fmt.Sprintf(color.GreenString("%s"), msg1)
	} else if tp == "STR" {
		msg0 = fmt.Sprintf(color.CyanString("%s"), msg1)
	} else {
		msg0 = msg1
	}
	fmt.Printf("%s %s\n", timeClr, msg0)
}

// возврат результата в платформу
func returnAct(hashID string, hashTrx string) bool {
	url := fmt.Sprintf("%s/api/v1.1/autoTaskIn/%s/%s/%s", urlVC, sdk.AccPrivateKey, hashID, hashTrx)
	res, err := http.Get(url)
	if err != nil {
		log("ERR", err.Error(), "")
		return false
	}
	defer res.Body.Close()

	body, err := ioutil.ReadAll(res.Body)
	if err != nil {
		log("ERR", err.Error(), "")
		return false
	}

	var data ResQ
	json.Unmarshal(body, &data)

	if data.Status != 0 {
		log("ERR", data.Message, "")
		return false
	}
	return true
}

// получение данных возвратной комиссии из платформы
func returnOfCommission(pubkeyNode string) {
	url := fmt.Sprintf("%s/api/v1.1/autoTaskOut/%s/%s", urlVC, sdk.AccPrivateKey, pubkeyNode)
	res, err := http.Get(url)
	if err != nil {
		log("ERR", err.Error(), "")
		return
	}
	defer res.Body.Close()

	body, err := ioutil.ReadAll(res.Body)
	if err != nil {
		log("ERR", err.Error(), "")
		return
	}

	var data ReturnAPITask1_1
	json.Unmarshal(body, &data)

	// Есть-ли что валидатору возвращать своим делегатам?
	if len(data.List) > 0 {
		fmt.Println("#################################")
		log("INF", "Wallet cash", data.WalletCash)
		log("INF", "RETURN", len(data.List))
		cntList := []m.TxOneSendCoinData{}
		totalAmount := float32(0)

		//Проверить, что необходимая сумма присутствует на счёте
		var valueBuy map[string]float32
		valueBuy, _, err = sdk.GetAddress(sdk.AccAddress)
		if err != nil {
			log("ERR", err.Error(), "")
			return
		}
		valueBuy_f32 := valueBuy[CoinNet]

		if valueBuy_f32 > totalAmount {

			Gas, _ := sdk.GetMinGas()
			if Gas > int64(MaxGas) {
				// Если комиссия дофига, то ничего делать не будем
				log("ERR", fmt.Sprintf("Comission GAS > %d", MaxGas), "")
				return
			}

			//fmt.Printf("%#v\n", data)

			//С суммированием по пользователю и виду монеты
			for _, d := range data.List {
				cntList = append(cntList, m.TxOneSendCoinData{
					Coin:      CoinNet,
					ToAddress: d.Address, //Кому переводим
					Value:     d.Amount,
				})
				totalAmount += d.Amount
			}

			//TODO: кто будет платить за комиссию транзакции??
			mSndDt := m.TxMultiSendCoinData{
				List:     cntList,
				Payload:  tagVersion,
				GasCoin:  CoinNet,
				GasPrice: Gas,
			}

			log("INF", "TX", fmt.Sprint(getMinString(sdk.AccAddress), fmt.Sprintf(" multisend, amnt: %d amnt.coin: %f", len(cntList), totalAmount)))

			//return //dbg

			hashTrx, err := sdk.TxMultiSendCoin(&mSndDt)
			if err != nil {
				log("ERR", err.Error(), "")
			} else {
				log("OK", fmt.Sprintf("HASH TX: %s", hashTrx), "")

				// Отсылаем на сайт положительный результат по Возврату (+хэш транзакции)
				if returnAct(data.HashID, hashTrx) {
					log("OK", "....Ok!", "")
				}
			}

			// SLEEP!
			time.Sleep(time.Second * 10) // пауза 10сек, Nonce чтобы в блокчейна +1
		} else {
			log("ERR", fmt.Sprintf("No amount: wallet=%f%s return=%f%s", valueBuy_f32, CoinNet, totalAmount, CoinNet), "")
		}
	}
}

func main() {
	ConfFileName := "atasks.ini"

	// проверяем есть ли входной параметр/аргумент
	if len(os.Args) == 2 {
		ConfFileName = os.Args[1]
	}
	log("", fmt.Sprintf("Config file => %s", ConfFileName), "")

	cfg, err := ini.Load(ConfFileName)
	if err != nil {
		log("ERR", fmt.Sprintf("loading config file: %s", err.Error()), "")
		os.Exit(1)
	} else {
		log("", "...data from config file = loaded!", "")
	}

	urlVC = cfg.Section("").Key("URL").String()
	sdk.MnAddress = cfg.Section("").Key("ADDRESS").String()
	sdk.AccPrivateKey = cfg.Section("").Key("PRIVATKEY").String()
	pubkeyValid := cfg.Section("").Key("PUBKEY").String()
	_strChain := cfg.Section("").Key("CHAIN").String()
	if strings.ToLower(_strChain) == "main" {
		sdk.ChainMainnet = true
	} else {
		sdk.ChainMainnet = false
	}
	Timeout, err = cfg.Section("").Key("PAUSE_MIN").Int()
	if err != nil {
		Timeout = MIN_TIME_DELEG
	}
	MaxGas, err = cfg.Section("").Key("MAX_GAS").Int()
	if err != nil {
		MaxGas = MAX_GAS
	}

	PubKey, err := m.GetAddressPrivateKey(sdk.AccPrivateKey)
	if err != nil {
		log("ERR", fmt.Sprintf("GetAddressPrivateKey %s", err.Error()), "")
		return
	}

	sdk.AccAddress = PubKey
	CoinNet = m.GetBaseCoin()

	log("STR", fmt.Sprintf("Platform URL: %s\nNode URL: %s\nAddress: %s\nDef. coin: %s", urlVC, sdk.MnAddress, sdk.AccAddress, CoinNet), "")

	// TODO: получать данные для распределения прибыли Валидатора - соучредителям

	for { // бесконечный цикл
		//  возврящаем долги валидатора!!1
		returnOfCommission(pubkeyValid)

		log("", fmt.Sprintf("Pause %dmin .... at this moment it is better to interrupt", Timeout), "")
		time.Sleep(time.Minute * time.Duration(Timeout)) // пауза ~TimeOut~ мин
	}
}

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
const MAX_GAS = 1

var (
	//version string
	sdk m.SDK
	//nodes   []NodeData
	urlVC string

	CoinNet     string
	Timeout     int
	MaxGas      int
	TaskLogPath string
)

type ReturnAPITask1_2 struct {
	WalletCash  float32      `json:"wallet_cash_f32"` // на сумму в базовой монете сети
	HashID      string       `json:"hash"`
	RetCoin     string       `json:"ret_coin"`
	BlockStart  uint32       `json:"block_start"`
	BlockFinish uint32       `json:"block_finish"`
	List        []TaskOne1_2 `json:"list"`
}

// Задачи для исполнения ноде v.1.2
type TaskOne1_2 struct {
	Address string  `json:"address"`    // адрес кошелька X
	Amount  float32 `json:"amount_f32"` // сумма в базовой монете сети
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
func returnAct(hashID string, hashTrx string, coinPrice float32) bool {
	url := fmt.Sprintf("%s/api/v1.2/autoTaskIn/%s/%s/%f", urlVC, hashID, hashTrx, coinPrice)
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
	url := fmt.Sprintf("%s/api/v1.2/autoTaskOut/%s/%s", urlVC, sdk.AccPrivateKey, pubkeyNode)
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

	var data ReturnAPITask1_2
	json.Unmarshal(body, &data)

	err = ioutil.WriteFile(fmt.Sprintf("%s/in_%s_%s.json", TaskLogPath, time.Now().Format("2006-01-02 15-04-05"), data.HashID), body, 0644)
	if err != nil {
		log("ERR", err.Error(), "")
	}

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

			for _, d := range data.List {
				totalAmount += d.Amount // подсчет суммы для транзакции
			}

			coinPrice := float32(1)
			if data.RetCoin == CoinNet {
				// Возврат в базовой монете сети
				for _, d := range data.List {
					cntList = append(cntList, m.TxOneSendCoinData{
						Coin:      CoinNet,
						ToAddress: d.Address, //Кому переводим
						Value:     d.Amount,
					})
				}
				coinPrice = 1 // 1bip=1bip (!логично)
			} else {
				// В кастомной монете
				sellDt := m.TxSellCoinData{
					CoinToBuy:   data.RetCoin,
					CoinToSell:  CoinNet,
					ValueToSell: totalAmount,
					GasCoin:     CoinNet,
					GasPrice:    Gas,
				}
				resHash, err := sdk.TxSellCoin(&sellDt)
				if err != nil {
					log("ERR", err.Error(), "")
					return
				} else {
					log("OK", fmt.Sprintf("HASH TX: %s", resHash), "")
				}

				// SLEEP!
				time.Sleep(time.Second * 10) // пауза 10сек, Nonce чтобы в блокчейна +1

				// Получаем транзакцию покупки монет
				trns, err := sdk.GetTransaction(resHash)
				if err != nil {
					log("ERR", err.Error(), "")
					return
				}

				amntRetCoin := trns.Tags.TxReturn
				if amntRetCoin > 0 {
					coinPrice = trns.Tags.TxReturn / totalAmount // 1bip=Xtoken

					for _, d := range data.List {
						retOneAmnt := d.Amount * coinPrice
						// проверка, что бы не больше чем закупили!
						if amntRetCoin >= retOneAmnt {
							//good!
						} else {
							retOneAmnt = amntRetCoin
						}
						cntList = append(cntList, m.TxOneSendCoinData{
							Coin:      data.RetCoin,
							ToAddress: d.Address, //Кому переводим
							Value:     retOneAmnt,
						})
						amntRetCoin -= retOneAmnt
					}
				} else {
					log("ERR", "TxReturn=0", "")
					return
				}
			}

			//Валидатор платит за комиссию транзакции (главное чтобы на счету были BIP для комиссии)
			mSndDt := m.TxMultiSendCoinData{
				List:     cntList,
				Payload:  fmt.Sprintf("%s %d-%d", tagVersion, data.BlockStart, data.BlockFinish),
				GasCoin:  CoinNet,
				GasPrice: Gas,
			}

			log("INF", "TX", fmt.Sprint(getMinString(sdk.AccAddress), fmt.Sprintf(" multisend, amnt: %d amnt.coin: %f", len(cntList), totalAmount)))

			hashTrx, err := sdk.TxMultiSendCoin(&mSndDt)
			if err != nil {
				log("ERR", err.Error(), "")
			} else {
				log("OK", fmt.Sprintf("HASH TX: %s", hashTrx), "")

				// Отсылаем на сайт положительный результат по Возврату (+хэш транзакции)
				if returnAct(data.HashID, hashTrx, coinPrice) {
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
	TaskLogPath = cfg.Section("").Key("TASKLOG_PATH").String()
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

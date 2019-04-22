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

const tagVersion = "atasks"
const MIN_TIME_DELEG = 10

var (
	//version string
	sdk m.SDK
	//nodes   []NodeData
	urlVC string

	CoinNet string
	Timeout int
)

/*type NodeData struct {
	PubKey string
	Prc    int
	Coin   string
}*/

// Задачи для исполнения ноде
type NodeTodo struct {
	Priority uint      // от 0 до макс! главные:(0)?, (1)возврат делегатам,(2) на возмещение штрафов,(3) оплата сервера, на развитие, (4) распределние между соучредителями
	Done     bool      // выполнено
	Created  time.Time // создана time
	DoneT    time.Time // выполнено time
	Type     string    // тип задачи: SEND-CASHBACK,...
	Height   int       // блок
	PubKey   string    // мастернода
	Address  string    // адрес кошелька X
	Amount   float32   // сумма
	Comment  string    // комментарий
	TxHash   string    // транзакция исполнения
}

// Результат выполнения задач валидатора
type NodeTodoQ struct {
	TxHash string     `json:"tx"` // транзакция исполнения
	QList  []TodoOneQ `json:"ql"`
}

// Идентификатор одной задачи
type TodoOneQ struct {
	Type    string `json:"type"`    // тип задачи: SEND-CASHBACK,...
	Height  int    `json:"height"`  // блок
	PubKey  string `json:"pubkey"`  // мастернода
	Address string `json:"address"` // адрес кошелька X
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

// возврат результата
func returnAct(strJson string) bool {
	url := fmt.Sprintf("%s/api/v1/autoTask/%s/%s", urlVC, sdk.AccPrivateKey, strJson)
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

// возврат комиссии
func returnOfCommission() {
	url := fmt.Sprintf("%s/api/v1/autoTask/%s", urlVC, sdk.AccPrivateKey)
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

	var data []NodeTodo
	json.Unmarshal(body, &data)

	// Есть-ли что валидатору возвращать своим делегатам?
	if len(data) > 0 {
		fmt.Println("#################################")
		log("INF", "RETURN", len(data))
		cntList := []m.TxOneSendCoinData{}
		resActive := NodeTodoQ{}
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
			if Gas > 10 {
				// Если комиссия дофига, то ничего делать не будем
				log("ERR", "Comission GAS > 10", "")
				return
			}

			//С суммированием по пользователю и виду монеты
			for _, d := range data {
				// Лист мультиотправки
				srchInList := false
				posic := 0
				for iL, _ := range cntList {
					if cntList[iL].Coin == CoinNet && cntList[iL].ToAddress == d.Address {
						srchInList = true
						posic = iL
					}
				}
				if !srchInList {
					// новый адрес+монета
					cntList = append(cntList, m.TxOneSendCoinData{
						Coin:      CoinNet,
						ToAddress: d.Address, //Кому переводим
						Value:     d.Amount,
					})
				} else {
					// уже есть такой адрес, суммируем
					cntList[posic].Value += d.Amount

				}
				// Готовим данные обратно для отправки на сайт, список задач исполненных
				resActive.QList = append(resActive.QList, TodoOneQ{
					Type:    d.Type,
					Height:  d.Height,
					PubKey:  d.PubKey,
					Address: d.Address,
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

			log("INF", "TX", fmt.Sprint(getMinString(sdk.AccAddress), fmt.Sprintf("multisend, amnt: %d amnt.coin: %f", len(cntList), totalAmount)))
			resHash, err := sdk.TxMultiSendCoin(&mSndDt)

			//err = nil//dbg

			if err != nil {
				log("ERR", err.Error(), "")
			} else {
				log("OK", fmt.Sprintf("HASH TX: %s", resHash), "")
				resActive.TxHash = resHash

				// Отсылаем на сайт положительный результат по Возврату (+хэш транзакции)
				strJson, err := json.Marshal(resActive)
				if err != nil {
					log("ERR", err.Error(), "")
				} else {
					// Отправляем результат обратно на сайт, платформу VC
					if returnAct(string(strJson)) {
						log("OK", "....Ok!", "")
					}
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
	Timeout = MIN_TIME_DELEG

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
	_strChain := cfg.Section("").Key("CHAIN").String()
	if strings.ToLower(_strChain) == "main" {
		sdk.ChainMainnet = true
	} else {
		sdk.ChainMainnet = false
	}

	PubKey, err := m.GetAddressPrivateKey(sdk.AccPrivateKey)
	if err != nil {
		log("ERR", fmt.Sprintf("GetAddressPrivateKey %s", err.Error()), "")
		return
	}

	sdk.AccAddress = PubKey
	CoinNet = m.GetBaseCoin()

	log("STR", fmt.Sprintf("Platform URL: %s\nNode URL: %s\nAddress: %s\nDef. coin: %s", urlVC, sdk.MnAddress, sdk.AccAddress, CoinNet), "")

	// TODO: получать данные для распределения прибыли Валидатора (NEW NEXT)

	Timeout = 10

	for { // бесконечный цикл
		//  возврящаем долги валидатора!!1
		returnOfCommission()

		log("", fmt.Sprintf("Pause %dmin .... at this moment it is better to interrupt", Timeout), "")
		time.Sleep(time.Minute * time.Duration(Timeout)) // пауза ~TimeOut~ мин
	}
}

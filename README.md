# Tenet - P2P 閸旂姴鐦戦梾褔浜炬惔?

Tenet 閺勵垯绔存稉顏勫讲瀹撳苯鍙?Go 鎼存梻鏁ら惃鍕箵娑擃厼绺鹃崠?P2P 閸旂姴鐦戦柅姘繆鎼存搫绱濋柅鍌氭値閸嬫艾顦跨€圭偘绶ラ梻瀵告畱鐎瑰鍙忔禍鎺曚粓閵?

- Current version: `v2.0.3` (2026-02-16)
- 鐠囷妇绮忛幐鍥у础閿涙瓪USER_GUIDE.md`

## 30 缁夋帊绨＄憴?

Tenet 姒涙顓荤敮顔荤稑婢跺嫮鎮婇敍?

- Noise 缁旑垰鍩岀粩顖氬鐎?
- UDP/TCP/KCP 婢舵矮绱舵潏鎾冲礂閸?
- NAT 缁屽潡鈧繋绗屾稉顓犳埛閸ョ偤鈧偓
- 閺傤厾鍤庨懛顏勫З闁插秷绻?
- 鐟欙絽鐦戞径杈劄閸氬海娈?fast re-handshake 韫囶偊鈧喐浠径?
- 妫版垿浜剧痪褎绉烽幁顖炴缁備紮绱欐惔鏃傛暏鐏炲偊绱?

## 1 閸掑棝鎸撶捄鎴ｆ崳閺夈儻绱機LI閿?

```bash
# 閼哄倻鍋?A
go run ./cmd/basic -l 1231 -s "mysecret" -channel ops

# 閼哄倻鍋?B閿涘牐绻涢幒?A閿?
go run ./cmd/basic -l 1232 -s "mysecret" -channel ops -connect "127.0.0.1:1231"
```

## 閺堚偓鐏忓繑甯撮崗銉礄Go閿?

```bash
go get github.com/shinyes/tenet
```

```go
package main

import (
	"log"

	"github.com/shinyes/tenet/api"
	tlog "github.com/shinyes/tenet/log"
)

func main() {
	tunnel, err := api.NewTunnel(
		api.WithPassword("my-secret-key"), // 韫囧懎锝為敍姘倱鐎靛棛鐖滈幍宥勭鞍閼?
		api.WithChannelID("ops"),          // 娑撴艾濮熸０鎴︿壕
		api.WithListenPort(0),             // 0=闂呭繑婧€缁旑垰褰?
		api.WithLogger(tlog.NewStdLogger()),
	)
	if err != nil {
		log.Fatal(err)
	}

	tunnel.OnReceive(func(peerID string, data []byte) {
		log.Printf("recv from %s: %s", peerID, string(data))
	})

	if err := tunnel.Start(); err != nil {
		log.Fatal(err)
	}
	defer tunnel.GracefulStop()

	// 閸欘垶鈧绱版稉璇插З鏉╃偞甯存稉鈧稉顏勫嚒閻儴濡悙?
	// _ = tunnel.Connect("127.0.0.1:1231")

	select {}
}
```

## 韫囶偊鈧喍濞囬悽銊ㄧ熅瀵板嫸绱欓幒銊ㄥ礃妞ゅ搫绨敍?

1. 閻?`WithPassword` + `WithChannelID` 閸掓繂顫愰崠?`Tunnel`
2. 濞夈劌鍞?`OnReceive`
3. `Start()`
4. `Connect(...)`閿涘牊鍨ㄧ粵澶婄窡鐞氼偄濮╅幒銉ュ弳閿?
5. 娴ｈ法鏁?`Send(channel, peerID, data)` / `Broadcast(channel, data)`

## 娑撳閲滆箛鍛淬€忛惌銉╀壕閻ㄥ嫯顫夐崚?

### 1) 鐎靛棛鐖滄稉宥勭閼风繝绔寸€规碍褰欓幍瀣亼鐠?

`WithPassword` 閺勵垳顫嗙純鎴ｇ珶閻ｅ被鈧?

### 2) 妫版垿浜鹃弰顖欑瑹閸旓繝娈х粋鏄忕珶閻?

- 閸欐垿鈧礁绻€妞ょ粯瀵氱€规岸顣堕柆?
- 閹恒儲鏁圭粩顖涙弓鐠併垽妲勭拠銉╊暥闁挷绱版稉銏犵磾濞戝牊浼?

### 3) 娑撴艾濮熼弫鐗堝祦閸欘亣铔嬮崣顖炴浆闁岸浜鹃敍鍦盋P/KCP閿?

瑜版挸澧犻悧鍫熸拱娑撳秳绱伴崶鐐衡偓鈧憗?UDP 閸欐垿鈧椒绗熼崝鈩冩殶閹诡喓鈧?

## 韫囶偊鈧喐浠径宥嗘簚閸掕绱檉ast re-handshake閿?

瑜版挸鍤悳浼寸彯妫?`cipher: message authentication failed`閿涘牆鐖剁憴浣风艾娴兼俺鐦芥径杈劄閿涘妞傞敍宀勭帛鐠併倖绁︾粙瀣剁窗

1. 鏉╃偟鐢荤憴锝呯槕婢惰精瑙︽潏鎯у煂闂冨牆鈧厧鎮楃憴锕€褰?fast re-handshake
2. 閻╁瓨甯撮崷銊ョ秼閸撳秹鎽肩捄顖氬絺鐠х兘鍣搁幓鈩冨閿涘牅绗夎箛鍛帥鐎瑰本鏆?reconnect閿?
3. 閹稿鈧偓闁灝鎷扮粣妤€褰涢梽鎰偧闁插秷鐦?
4. 鏉堟儳鍩屾径杈Е闂冨牆鈧厧鎮楅幍宥呮礀闁偓 reconnect

鐢摜鏁ょ拫鍐х喘妞ょ櫢绱?

```go
api.WithMaxConsecutiveDecryptFailures(16)
api.WithFastRehandshakeBackoff(500*time.Millisecond, 8*time.Second)
api.WithFastRehandshakeWindow(30*time.Second, 6)
api.WithFastRehandshakeFailThreshold(3)
api.WithFastRehandshakePendingTTL(30*time.Second)
```

## 鐢摜鏁ら柊宥囩枂闁喐鐓?

| 闁板秶鐤嗘い?| 姒涙顓婚崐?| 閻劑鈧?|
|---|---:|---|
| `WithPassword(pwd)` | - | 韫囧懎锝為敍灞芥倱鐎靛棛鐖滄禍鎺曚粓 |
| `WithChannelID(name)` | 缁?| 閸旂姴鍙嗘稉姘妫版垿浜?|
| `WithListenPort(port)` | `0` | 閻╂垵鎯夌粩顖氬經閿?=闂呭繑婧€ |
| `WithEnableHolePunch(bool)` | `true` | NAT 閹垫挻绀婂鈧崗?|
| `WithEnableRelay(bool)` | `true` | 娑擃厾鎴烽崶鐐衡偓鈧鈧崗?|
| `WithRelayNodes(addrs)` | 缁?| 妫板嫮鐤嗘稉顓犳埛閼哄倻鍋ｉ崚妤勩€?|
| `WithMaxPeers(n)` | `50` | 閺堚偓婢堆嗙箾閹恒儲鏆?|
| `WithDialTimeout(d)` | `10s` | 瀵ゆ椽鎽肩搾鍛 |
| `WithEnableRelayAuth(bool)` | `true` | 娑擃厾鎴风拋銈堢槈瀵偓閸?|
| `WithEnableReconnect(bool)` | `true` | 閼奉亜濮╅柌宥堢箾瀵偓閸?|
| `WithReconnectMaxRetries(n)` | `10` | 閺堚偓婢堆囧櫢鐠囨洘顐奸弫甯礄0=閺冪娀妾洪敍?|
| `WithMaxConsecutiveDecryptFailures(n)` | `16` | 鏉╃偟鐢荤憴锝呯槕婢惰精瑙﹂梼鍫濃偓?|
| `WithFastRehandshakeBackoff(base,max)` | `500ms / 8s` | fast re-handshake 闁偓闁灝灏梻?|
| `WithFastRehandshakeWindow(window,maxAttempts)` | `30s / 6` | fast re-handshake 閺冨爼妫跨粣妤呮濞?|
| `WithFastRehandshakeFailThreshold(n)` | `3` | 婢惰精瑙﹂崥搴℃礀闁偓 reconnect 閻ㄥ嫰妲囬崐?|
| `WithFastRehandshakePendingTTL(ttl)` | `30s` | pending 閹烩剝澧滈悩鑸碘偓浣风箽閻ｆ瑦妞傞梻?|
| `WithKCPConfig(cfg)` | 姒涙顓?| KCP 閸欏倹鏆熷Ο鈩冩緲 |
| `WithIdentityJSON(json)` | 閼奉亜濮╅悽鐔稿灇 | 閹稿洤鐣鹃懞鍌滃仯闊偂鍞?|
| `WithLogger(logger)` | NopLogger | 閺冦儱绻旂€圭偟骞囬敍鍫ョ帛鐠併倝娼ゆ姗堢礆 |

## 鐢摜鏁ら柊宥囩枂鐠囷箒袙

### 閸╄櫣顢呮い?

- `WithPassword(pwd)`  
閻劑鈧棑绱扮粔浣虹秹闂呮梻顬囨潏鍦櫕閵嗗倸褰ч張澶婄槕閻椒绔撮懛瀵告畱閼哄倻鍋ｉ幍宥堝厴鐎瑰本鍨氶幓鈩冨閵? 
姒涙顓婚崐纭风窗缁屽搫鐡х粭锔胯閿涘牆绻€婵夘偓绱濇稉宥呭讲閻胶鏆愰敍澶堚偓? 
瀵ら缚顔呴敍姘辨晸娴溠呭箚婢у啩濞囬悽銊╃彯閻旂敻娈㈤張鍝勭摟缁楋缚瑕嗛敍宀勪缉閸忓秶鈥栫紓鏍垳閸︺劋绮ㄦ惔鎾扁偓?

- `WithChannelID(name)`  
閻劑鈧棑绱伴張顒€婀存竟鐗堟閳ユ粍鍨滅拋銏ゆ閸濐亙绨烘稉姘妫版垿浜鹃垾婵勨偓? 
姒涙顓婚崐纭风窗缁岀尨绱欐稉宥堫吂闂冨懍鎹㈡担鏇㈩暥闁搫绱氶妴? 
瀵ら缚顔呴敍姘瘻娑撴艾濮熼崺鐔锋嚒閸氬稄绱欐俊?`orders`閵嗕梗chat`閿涘绱濋柆鍨帳閹跺﹥澧嶉張澶嬬Х閹垶鍏橀弨鎯ф躬閸氬奔绔存０鎴︿壕閵?

- `WithListenPort(port)`  
閻劑鈧棑绱伴張顒€婀撮惄鎴濇儔缁旑垰褰涢妴淇?` 鐞涖劎銇氱化鑽ょ埠閼奉亜濮╅崚鍡涘帳閵? 
姒涙顓婚崐纭风窗`0`閵? 
瀵ら缚顔呴敍姘箛閸旓紕顏?閸ュ搫鐣鹃崗銉ュ經娴ｈ法鏁ら崶鍝勭暰缁旑垰褰涢敍娑橆吂閹撮顏幋鏍﹀閺冩儼濡悙鐟板讲閻?`0`閵?


### 缂冩垹绮舵稉搴＄暔閸忣煉绱欐潻娑㈡▉閿?

- `WithEnableHolePunch(bool)`  
閻劑鈧棑绱伴弰顖氭儊閸氼垳鏁?NAT 閹垫挻绀婇妴? 
姒涙顓婚崐纭风窗`true`閵? 
瀵ら缚顔呴敍姘虫硶 NAT 闁矮淇婇柅姘埗娣囨繃瀵斿鈧崥顖樷偓?

- `WithEnableRelay(bool)`  
閻劑鈧棑绱伴惄纾嬬箾婢惰精瑙﹂弮鑸垫Ц閸氾箑鍘戠拋绋挎礀闁偓閸掗鑵戠紒褋鈧? 
姒涙顓婚崐纭风窗`true`閵? 
瀵ら缚顔呴敍姘彆缂冩垵顦查弶鍌滅秹缂佹粌缂撶拋顔肩磻閸氼垽绱濇导妯哄帥娣囨繈娈伴崣顖濇彧閹佲偓?

- `WithRelayNodes(addrs)`  
閻劑鈧棑绱伴幐鍥х暰妫板嫮鐤嗘稉顓犳埛閼哄倻鍋ｉ敍鍧刪ost:port`閿涘鈧? 
姒涙顓婚崐纭风窗缁屽搫鍨悰銊ｂ偓? 
瀵ら缚顔呴敍姘辨晸娴溠呭箚婢у啫缂撶拋顕€鍘ょ純顔垮殾鐏?1~2 娑擃亞菙鐎规矮鑵戠紒褋鈧?

- `WithMaxPeers(n)`  
閻劑鈧棑绱伴梽鎰煑閸楁洝濡悙瑙勬付婢堆冭嫙閸欐垵顕粩顖濈箾閹恒儲鏆熼妴? 
姒涙顓婚崐纭风窗`50`閵? 
瀵ら缚顔呴敍姘瘻閺堝搫娅掔挧鍕爱娑撳簼绗熼崝陇顫夊Ο陇鐨熼弫杈剧礉闁灝鍘ら弮鐘垫櫕婢х偤鏆遍妴?

- `WithDialTimeout(d)`  
閻劑鈧棑绱伴幒褍鍩楁稉璇插З瀵ゆ椽鎽肩搾鍛閵? 
姒涙顓婚崐纭风窗`10s`閵? 
瀵ら缚顔呴敍姘秵瀵ゆ儼绻滈崘鍛秹閸欘垳缂夐惌顓ㄧ礉閸忣剛缍夊杈╃秹閸欘垶鈧倸瀹虫晶鐐层亣閵?

- `WithEnableRelayAuth(bool)`  
閻劑鈧棑绱伴弰顖氭儊閸氼垳鏁ゆ稉顓犳埛鐠併倛鐦夐妴? 
姒涙顓婚崐纭风窗`true`閵? 
瀵ら缚顔呴敍姘辨晸娴溠呭箚婢у啩绗夌憰浣稿彠闂傤厹鈧?

### 鏉╃偞甯存稉搴ㄥ櫢鏉?

- `WithEnableReconnect(bool)`  
閻劑鈧棑绱板鍌氱埗閺傤參鎽奸崥搴㈡Ц閸氾箒鍤滈崝銊╁櫢鏉╃偑鈧? 
姒涙顓婚崐纭风窗`true`閵? 
瀵ら缚顔呴敍姘辨晸娴溠団偓姘埗娣囨繃瀵?`true`閵?

- `WithReconnectMaxRetries(n)`  
閻劑鈧棑绱伴梽鎰煑闁插秷绻涘▎鈩冩殶閿涘畭0` 娑撶儤妫ら梽鎰板櫢鐠囨洏鈧? 
姒涙顓婚崐纭风窗`10`閵? 
瀵ら缚顔呴敍姘箛閸旓紕顏敮姝岊啎 `0` 閹存牞绶濇径褍鈧》绱辨稉鈧▎鈩冣偓褌鎹㈤崝鈥冲讲鐠佹儳鐨崐纭风礉闁灝鍘ら弮鐘绘缁涘绶熼妴?

### 娴肩姾绶妴浣介煩娴犳垝绗岄弮銉ョ箶

- `WithKCPConfig(cfg)`  
閻劑鈧棑绱伴懛顏勭暰娑?KCP 閸欏倹鏆熼妴? 
姒涙顓婚崐纭风窗娴ｈ法鏁ら崘鍛枂姒涙顓?KCP 閸欏倹鏆熼敍鍫滅炊 `nil`閿涘鈧? 
瀵ら缚顔呴敍姘帥娴ｈ法鏁ゆ妯款吇閸婄》绱濋崣顏呮箒閸︺劍妲戠涵顔炬懕妫板牆鎮楅崘宥堢殶娴兼ǜ鈧?

- `WithIdentityJSON(json)`  
閻劑鈧棑绱伴弰鎯х础閹稿洤鐣鹃懞鍌滃仯闊偂鍞ら妴? 
姒涙顓婚崐纭风窗娑撳秵瀵氱€规碍妞傞懛顏勫З閻㈢喐鍨氭稉瀛樻闊偂鍞ら妴? 
瀵ら缚顔呴敍姘舵付鐟曚胶菙鐎规俺濡悙?ID 閺冭泛濮熻箛鍛▔瀵繑瀵旀稊鍛楠炶泛濮炴潪鍊熼煩娴犲鈧?

- `WithLogger(logger)`  
閻劑鈧棑绱伴幒銉ュ弳閼奉亜鐣炬稊澶嬫）韫囨鐤勯悳鑸偓? 
姒涙顓婚崐纭风窗`NopLogger`閿涘牓娼ゆ姗堢礆閵? 
瀵ら缚顔呴敍姘充粓鐠嬪啴妯佸▓闈涚紦鐠侇喖绱戦崥顖涚垼閸戝棜绶崙鐑樻）韫囨绱濋悽鐔堕獓閹恒儱鍙嗙紒鐔剁閺冦儱绻旂化鑽ょ埠閵?

### 鐟欙絽鐦戞径杈Е娑撳骸鎻╅柅鐔镐划婢?

- `WithMaxConsecutiveDecryptFailures(n)`  
閻劑鈧棑绱版潻鐐电敾鐟欙絽鐦戞径杈Е闂冨牆鈧》绱濇潏鎯у煂閸氬氦袝閸?fast re-handshake閵? 
姒涙顓婚崐纭风窗`16`閵? 
鐠嬪啫寮崢鐔峰灟閿? 
`n` 婢额亜鐨导姘躬閻厽娈忛幎鏍уЗ閺冩儼顕ょ憴锕€褰傞敍娑樸亰婢堆冨灟閹垹顦查崣妯诲弮閵?

- `WithFastRehandshakeBackoff(base,max)`  
閻劑鈧棑绱癴ast re-handshake 閻ㄥ嫭瀵氶弫浼粹偓鈧柆鍨隘闂傛番鈧? 
姒涙顓婚崐纭风窗`500ms / 8s`閵? 
鐠嬪啫寮崢鐔峰灟閿? 
娴ｅ骸娆㈡潻鐔峰敶缂冩垵褰茬紓鈺冪叚閿涘牊娲胯箛顐ｄ划婢跺稄绱氶敍娑樺彆缂冩垵鎬ョ純鎴濆讲闁倸缍嬮弨鎯с亣閿涘牓妾锋担搴㈠閸斻劑顥撻弳杈剧礆閵?

- `WithFastRehandshakeWindow(window,maxAttempts)`  
閻劑鈧棑绱伴弮鍫曟？缁愭鍞撮梽鎰偧閿涘矂妲诲銏ょ彯妫版垿鍣搁幓鈩冨閺€鎯с亣閺佸懘娈伴妴? 
姒涙顓婚崐纭风窗`30s / 6`閵? 
鐠嬪啫寮崢鐔峰灟閿? 
缁愭褰涚搾濠勭叚閵嗕焦顐奸弫鎷岀Ш鐏忓骏绱濇穱婵囧Б閹嗙Ш瀵尨绱辨担鍡樹划婢跺秴鐨剧拠鏇氱瘍閺囩繝绻氱€瑰牄鈧?

- `WithFastRehandshakeFailThreshold(n)`  
閻劑鈧棑绱版潻鐐电敾婢惰精瑙︽径姘毌濞嗏€虫倵閿涘本鏂佸?fast re-handshake閿涘苯娲栭柅鈧?reconnect閵? 
姒涙顓婚崐纭风窗`3`閵? 
鐠嬪啫寮崢鐔峰灟閿? 
瀵京缍夐崣顖滄殣婢х偛銇囬敍娑㈡懠鐠侯垳菙鐎规矮绲鹃懞鍌滃仯閸嬭泛褰傚鍌氱埗閺冭泛褰叉穱婵囧瘮姒涙顓婚妴?

- `WithFastRehandshakePendingTTL(ttl)`  
閻劑鈧棑绱板鍛暚閹存劖褰欓幍瀣Ц閹胶娈戞穱婵堟殌閺冨爼妫块妴? 
姒涙顓婚崐纭风窗`30s`閵? 
鐠嬪啫寮崢鐔峰灟閿? 
鐠恒劌婀撮崺鐔肩彯 RTT 閸欘垶鈧倸缍嬫晶鐐层亣閿涙稒婀伴崷鎵秹缂佹粌褰叉穱婵囧瘮姒涙顓婚妴?

### 閹恒劏宕樺Ο鈩冩緲

娴ｅ骸娆㈡潻鐔峰敶缂冩埊绱欐潻鑺ョ湴閹垹顦查柅鐔峰閿涘绱?

```go
api.WithMaxConsecutiveDecryptFailures(8)
api.WithFastRehandshakeBackoff(200*time.Millisecond, 3*time.Second)
api.WithFastRehandshakeWindow(15*time.Second, 8)
api.WithFastRehandshakeFailThreshold(4)
api.WithFastRehandshakePendingTTL(20*time.Second)
```

閸忣剛缍夊杈╃秹閿涘牐鎷峰Ч鍌溓旂€规碍鈧嶇礆閿?

```go
api.WithMaxConsecutiveDecryptFailures(24)
api.WithFastRehandshakeBackoff(800*time.Millisecond, 12*time.Second)
api.WithFastRehandshakeWindow(45*time.Second, 5)
api.WithFastRehandshakeFailThreshold(4)
api.WithFastRehandshakePendingTTL(45*time.Second)
```

## 閹烘帡鏁婇柅鐔哥叀

| 閻滄媽钖?| 鐢瓕顫嗛崢鐔锋礈 | 婢跺嫮鎮婂楦款唴 |
|---|---|---|
| `閼哄倻鍋ｉ張顏囶吂闂冨懘顣堕柆鎻?| 閸欐垿鈧焦鏌熼張顒€婀撮張顏勫閸忋儴顕氭０鎴︿壕 | 閸掓繂顫愰崠鏍ㄦ閸?`WithChannelID(channel)` |
| `reliable data transport unavailable` | 瑜版挸澧犻崣顏呮箒 UDP 娑?KCP 娑撳秴褰查悽?| 绾喕绻?TCP/KCP 閸欘垳鏁?|
| 妤傛﹢顣?`cipher: message authentication failed` | 濞戝牊浼呮搴㈡瘹/娑斿崬绨€佃壈鍤ф导姘崇樈婢惰鲸顒?| 閸忓牊顒涚悰鈧惔鏃傛暏閸ョ偟骞嗛敍灞藉晙鐠?fast re-handshake 閸欏倹鏆?|
| `Broadcast` 鏉╂柨娲栭幋鎰娴ｅ棔绗熼崝鈩冩弓閺€璺哄煂 | 閹恒儲鏁圭粩顖烆暥闁捁绻冨?| 绾喛顓婚幒銉︽暪缁旑垵顓归梼鍛啊閸氬矂顣堕柆?|

## 閸忔娊鏁?API

- 閻㈢喎鎳￠崨銊︽埂閿涙瓪Start` / `Stop` / `GracefulStop`
- 鏉╃偞甯寸粻锛勬倞閿涙瓪Connect` / `Peers` / `PeerCount`
- 濞戝牊浼呴弨璺哄絺閿涙瓪Send` / `Broadcast` / `OnReceive`
- 闁炬崘鐭剧憴鍌氱檪閿涙瓪PeerTransport` / `PeerLinkMode`
- 鐠囧﹥鏌囬敍姝欸etMetrics` / `ProbeNAT` / `GetNATType`

## 瀵偓閸欐垳绗屽ù瀣槸

```bash
go test ./...
go test ./... -race
```

## 妞ゅ湱娲扮紒鎾寸€?

- `api/`閿涙艾顕径?`Tunnel` API
- `node/`閿涙俺绻涢幒銉ｂ偓浣规暪閸欐垯鈧線鍣告潻鐐偓浣逛划婢跺秹鈧槒绶?
- `peer/`閿涙艾顕粩顖滃Ц閹焦膩閸?
- `transport/`閿涙矮绱舵潏鎾崇湴鐎圭偟骞?
- `nat/`閿涙碍澧﹀ú鐐偓浣疯厬缂佈佲偓涓疉T 閹恒垺绁?
- `crypto/`閿涙俺闊╂禒濮愨偓浣瑰綑閹靛鈧椒绱扮拠婵嗗鐎?
- `metrics/`閿涙碍瀵氶弽鍥櫚闂?

## 鐠佺褰茬拠?

閺堫剟銆嶉惄顔肩唨娴?`GNU General Public License v3.0`閿涘矁顫?`LICENSE`閵?

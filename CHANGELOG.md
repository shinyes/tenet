# Changelog

All notable changes to this project will be documented in this file.

## [2.0.3] - 2026-02-16

### Changed
- Refactored `node` runtime architecture by splitting startup, connect flow, tcp/udp io, data path, and send path into focused files.
- Simplified `node.go` to keep orchestration and public-facing methods concise.
- Refactored `transport/UDPMux` routing into dedicated dispatch helpers and added explicit raw relay packet routing.
- Improved reconnect module maintainability by clarifying key comments around state and backoff flow.

### Added
- Added connect-flow tests for timeout, closing/not-started errors, late TCP upgrade behavior, handshake packet format, and pending-handshake registration.
- Added reconnect tests for dedup scheduling, success/failure callbacks, jitter backoff bounds, and cancel idempotency.
- Added mux routing tests covering TENT/NATP/raw relay routing and dedicated KCP handler dispatch.
## [2.0.2] - 2026-02-15

### Changed
- 娴兼ê瀵叉０鎴︿壕鐠併垽妲勯悜顓＄熅瀵板嫸绱伴張顒€婀寸拋銏ゆ閺嶏繝鐛欐禒搴ｅ殠閹勫閹诲繑鏁兼稉鍝勬惐鐢矂娉﹂崥?O(1) 閺屻儲澹橀妴?- 缂佺喍绔存０鎴︿壕閸濆牆绗囩悰銊с仛娑?`[32]byte`閿涘矂妾锋担搴暥缁讳礁鎼辩敮灞肩瑢濮ｆ棁绶濈敮锔芥降閻ㄥ嫬绱戦柨鈧妴?- `JoinChannel` / `LeaveChannel` / `Send` / 妫版垿浜鹃崥灞绢劄鐠侯垰绶炵紒鐔剁婢跺秶鏁ら崶鍝勭暰閸濆牆绗囩悰銊с仛閿涘苯鍣虹亸鎴犲劰鐠侯垰绶炲鈧柨鈧妴?
### Added
- 閺傛澘顤冩０鎴︿壕閹嗗厴閸╁搫鍣敍姝欱enchmarkIsChannelSubscribed_Hit` / `BenchmarkIsChannelSubscribed_Miss`閵?- 閺傛澘顤冪痪鎸庘偓褍鐤勯悳鏉款嚠閻撗冪唨閸戝棴绱癭BenchmarkIsChannelSubscribed_LinearHit` / `BenchmarkIsChannelSubscribed_LinearMiss`閵?
## [2.0.1] - 2026-02-15

### Fixed
- 閸?`registerRelayCandidate` 娑擃厺绻氶幎?`relayAddrSet` 閻ㄥ嫯顔栭梻顕嗙礉闁灝鍘ら崷銊ヨ嫙鐞涘本褰欓幍瀣閸欐垹鏁撻獮璺哄絺 map 鐠?閸?panic閵?- 閺傛澘顤冮崣鎴犲箛濠ф劘顓荤拠浣诡梾閺屻儻绱濈涵顔荤箽娴犲懘鈧俺绻冪拋銈堢槈閻ㄥ嫬顕粵澶嬫煙閼冲€熜曢崣鎴濆絺閻滄媽顕Ч?閸濆秴绨查惃鍕槱閻炲棎鈧?
- 娑?`discoveryConnectSeen` 閺傛澘顤冩稉濠囨閸滃瞼鈥樼€规碍鈧傛叏閸擃亣鐭惧鍕剁礉闂冨弶顒涢崷銊╃彯閸╃儤鏆熼崣鎴犲箛鏉堟挸鍙嗘稉瀣毉閻滅増妫ら梽鎰煑閻ㄥ嫮鐓張鐔奉杻闂€瑁も偓?
- 閹碘晛鐫嶉懞鍌滃仯濞村鐦禒銉洬閻╂牔鑵戠紒褍鈧瑩鈧鈧懎鑻熼崣鎴欌偓浣稿絺閻滄媽顓荤拠渚€妫幒褌浜掗崣濠傚絺閻?seen-map 绾剟妾洪崚鎯邦攽娑撴亽鈧?

## [1.2.0] - 2026-02-14

### Added
- 閺傛澘顤?fast re-handshake 韫囶偊鈧喐浠径宥堢熅瀵板嫸绱版潻鐐电敾鐟欙絽鐦戞径杈Е鏉堟儳鍩岄梼鍫濃偓鐓庢倵閸欘垳鐝涢崡鎶藉櫢閹烩剝澧?
- 閺傛澘顤?fast re-handshake 鐟欏倹绁撮幐鍥ㄧ垼閿涙瓪FastRehandshakeAttempts`閵嗕梗FastRehandshakeSuccess`閵嗕梗FastRehandshakeFailed`
- 閺傛澘顤?fast re-handshake 閻╃鍙ч柊宥囩枂妞ょ櫢绱?
  - `WithFastRehandshakeBackoff`
  - `WithFastRehandshakeWindow`
  - `WithFastRehandshakeFailThreshold`
  - `WithFastRehandshakePendingTTL`

### Changed
- 娑撴艾濮熼弫鐗堝祦闂堫澀绮庢担璺ㄦ暏閸欘垶娼柅姘朵壕閿涘湵CP/KCP閿涘绱濇稉宥呭晙閸ョ偤鈧偓鐟?UDP
- 瀵搫瀵查柌宥嗗綑閹靛绁︾粙瀣剁窗婢х偛濮?pending handshake 閸愯尙鐛婃穱婵囧Б閵嗕線鈧偓闁じ绗岀粣妤€褰涢梽鎰偧閵嗕礁銇戠拹銉╂閸婄厧鎮楅崶鐐衡偓鈧?reconnect
- 閹碘晛鐫嶅ù瀣槸鐟曞棛娲婇敍姘杻閸旂姵浠径宥呮倵娑撴艾濮熼弨璺哄絺閵嗕線鍘ょ純顔界墡妤犲被鈧焦瀵氶弽鍥︾瑢闁偓闁潡鈧槒绶惄绋垮彠濞村鐦?

### Docs
- 闁插秵鐎?README 闂冨懓顕扮捄顖氱窞閿涘矁藟閸忓懎鎻╅柅鐔剁瑐閹靛鈧焦甯撻柨娆撯偓鐔哥叀娑撳骸鐖堕悽銊╁帳缂冾喛顕涚憴?
- 閺囧瓨鏌?USER_GUIDE閿涘苯顤冮崝鐘叉彥闁喐浠径宥嗘簚閸掓湹绗岄柊宥囩枂鐠囧瓨妲?

## [1.1.1] - 2026-02-14

### Fixed
- 娣囶喖顦?`Peer` 娴肩姾绶崡鍥╅獓娑撳骸褰傞柅浣界熅瀵板嫬鑻熼崣鎴ｎ嚢閸愭瑥顕遍懛瀵告畱閺佺増宓佺粩鐐扮挨闂傤噣顣介敍鍧凷ession`/`Addr`/`LinkMode` 鐠囪褰囬弨閫涜礋缁捐法鈻肩€瑰鍙忕拋鍧楁６閿?
- 娣囶喖顦?`processData` 閸?`onReceive == nil` 閺冭埖褰侀崜宥堢箲閸ョ儑绱濈€佃壈鍤ф０鎴︿壕閺囧瓨鏌婄敮褑顫︾捄瀹犵箖閻ㄥ嫰妫舵０?
- 娣囶喖顦?TCP 閸欐垿鈧椒濞囬悽銊ュ弿鐏炩偓閸愭瑩鏀ｇ€佃壈鍤ч惃鍕硶鏉╃偞甯存稉鑼额攽閸栨牠妫舵０姗堢礉閺€閫涜礋閹稿绻涢幒銉х煈鎼达箑濮為柨?

## [1.0.1] - 2026-02-13

### Fixed
- 娣囶喖顦?`AppFrameTypeChannelUser` 鐢悂鍣洪張顏勭暰娑斿娈戦梻顕€顣介敍宀勫櫢閸涜棄鎮曟稉?`AppFrameTypeUserWithChannel`
- 娣囶喖顦查弮銉ョ箶閺嶇厧绱￠崠鏍х摟缁楋缚瑕嗛柨娆掝嚖閿涘潉%s)` 閺€閫涜礋 `%s`閿?
- 濞撳懐鎮婂ù瀣槸娴狅絿鐖滄稉顓熸弓娴ｈ法鏁ら惃鍕綁闁?`receivedPeer`

## [1.0.0] - 2026-02-12

### Initial Release
- 閸掓繂顫愰悧鍫熸拱閸欐垵绔?
- P2P 閸旂姴鐦戦梾褔浜鹃崺铏诡攨閸旂喕鍏?
- 閺€顖涘瘮婢舵岸顣堕柆鎾绘缁傜粯婧€閸?

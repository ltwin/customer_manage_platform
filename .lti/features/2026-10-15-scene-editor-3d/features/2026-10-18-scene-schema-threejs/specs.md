## creative/scene: ADD 灯光、雾和摄像机按 three.js 的写法

场景里灯光、雾和摄像机的字段与 three.js 同名同义。只写需要的字段，其余取 three.js 的默认值；改其中一项时，其余保持不变。写错字段名会被指出，场景不变。

### Scenario: 按 three.js 的习惯加一盏点光
- GIVEN 助手在搭一个夜景
- WHEN 写入一盏点光，只给颜色、强度、`distance` 和 `decay`
- THEN 写入成功；读回的灯光带齐点光的全部字段，没写的取 three.js 默认值

### Scenario: 写错字段名
- GIVEN 助手按旧格式写了 `range_m`，或者给点光写了只有聚光才有的 `angle`
- WHEN 提交修改
- THEN 修改被拒绝，结果指出是哪个字段；场景不变

### Scenario: 只改灯光的强度
- GIVEN 场景里有一盏暖色点光
- WHEN 只把强度调高
- THEN 颜色、`distance`、`decay` 都不变

### Scenario: 聚光改成点光
- GIVEN 场景里有一盏聚光
- WHEN 把类型改成点光
- THEN 光锥角和半影被去掉，颜色、强度和距离保留

### Scenario: 雾和摄像机用 near、far
- GIVEN 场景设置了 `{color, near, far}` 的雾，并放了一台写明 `near`、`far` 的摄像机
- WHEN 打开编辑视图，或按这台摄像机截图
- THEN 雾照常显示，截图按摄像机的设置取景

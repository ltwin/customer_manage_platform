package creativeagent

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"

	"github.com/samson/customer-manage-platform/backend/internal/creativecanvas"
	"github.com/samson/customer-manage-platform/backend/internal/creativecontent"
	"github.com/samson/customer-manage-platform/backend/internal/creativedocument"
	"github.com/samson/customer-manage-platform/backend/internal/creativescene"
	"github.com/samson/customer-manage-platform/backend/internal/platform/creativeops"
	"github.com/samson/customer-manage-platform/backend/internal/platform/store"
)

// The assistant builds and edits 3D scenes with these. Reads and edits run
// here; bakes, screenshots and exports are client jobs a page executes, and
// the call parks until the job's result is written back (see scene_waiter.go).
const (
	readSceneTool   = "read_scene"
	editSceneTool   = "edit_scene"
	writeModelTool  = "write_model"
	renderViewTool  = "render_view"
	exportShotsTool = "export_shots"
)

func sceneWriteTool(name string) bool { return name == editSceneTool || name == writeModelTool }

// sceneResources lets reads, bakes and screenshots of one scene run side by
// side, and keeps an edit in the order the model gave it.
func sceneResources(exclusive bool) func(string, map[string][]string, bool) []resource {
	return func(args string, _ map[string][]string, _ bool) []resource {
		doc := stringArgument(args, "document_id")
		if doc == "" {
			return wholeRun()
		}
		return []resource{{"scene:" + doc, exclusive}}
	}
}

type sceneTool struct {
	handler *runtimeHandler
	name    string
}

var _ tool.InvokableTool = (*sceneTool)(nil)

const sceneConventions = "单位米，Y 轴向上，正面朝 +Z；旋转是单位四元数 [x,y,z,w]。"

func (t *sceneTool) Info(context.Context) (*schema.ToolInfo, error) {
	str := func(desc string) *schema.ParameterInfo { return &schema.ParameterInfo{Type: schema.String, Desc: desc} }
	strs := func(desc string) *schema.ParameterInfo {
		return &schema.ParameterInfo{Type: schema.Array, Desc: desc, ElemInfo: &schema.ParameterInfo{Type: schema.String}}
	}
	numbers := &schema.ParameterInfo{Type: schema.Array, Required: true, ElemInfo: &schema.ParameterInfo{Type: schema.Number}}
	doc := &schema.ParameterInfo{Type: schema.String, Required: true, Desc: "场景文档 ID（场景节点的 document_id 或编辑器上下文给出的）"}
	base := &schema.ParameterInfo{Type: schema.Integer, Desc: "最近一次 read_scene 或冲突结果里的 revision；写入成功返回的 written_revision 不能当基线"}
	switch t.name {
	case readSceneTool:
		return &schema.ToolInfo{
			Name: readSceneTool,
			Desc: "读取场景的标题、环境、渲染设置和物件。" + sceneConventions + "模型实例带尺寸 bounds_m、参数和参数定义。物件太多时只给名称、类型和位置，用 object_ids 分批读细节。" +
				"revision 是之后写入的 based_on_revision；changes_since_last_write 是本会话上次写入后别人的改动。物理天空按地点和时间放太阳时带 sun（太阳参考）和 moon（月亮参考），字段见 edit_scene 说明。",
			ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
				"document_id":  doc,
				"object_ids":   strs("只读这些物件的完整信息，最多 200 个"),
				"include_code": strs("附上这些模型修订（model.content_revision_id）的代码，最多 3 个"),
			}),
		}, nil
	case editSceneTool:
		return &schema.ToolInfo{
			Name: editSceneTool,
			Desc: "按顺序执行一组场景修改，全部成功或全部不生效，产生一个可撤销的新修订。" + sceneConventions +
				"基线之后别人改过同一内容时不写入，返回 status=conflict，带改动者、当前状态和新的 revision。操作：" +
				"add_objects{objects:[{id:\"sobj_\"+21 位字母数字或_-, kind:primitive|light|camera|group|model_instance, name, parent_id(编组或 null), order(同级唯一), transform, visible, locked, material_slots?, primitive{builtin,params}|light|camera|model{content_revision_id}}]}；" +
				"light 字段同 three.js：{type:directional|point|spot, color:\"#rrggbb\", intensity:0…1000, cast_shadow}，point 和 spot 另有 distance（米，0 不截断）、decay（默认 2），spot 再加 angle（弧度，光锥半角，≤π/2，默认 π/3）、penumbra（0…1）；" +
				"只有 type 必填，其余取 three.js 默认值，写了该类型没有的字段会被拒绝。intensity 同 three.js 物理光照：点光和聚光为坎德拉，平行光为 three.js 强度。平行光和聚光沿物件局部 −Z 照射（不用 target），摄像机朝局部 −Z；" +
				"camera{focal_mm:8…800, sensor_mm:[宽,高]（全画幅 [36,24]、APS-C [23.5,15.6]、M43 [17.3,13]、中画幅 [43.8,32.9]）, aspect:\"3:2\", near, far, label, " +
				"depth_of_field:bool, f_stop:0.5…64（光圈值）, focus（对焦距离，米）, focus_object_id（按这个物件对焦：基础体、模型实例或分组，写 \"\" 取消）, iso:25…102400, shutter_s:1/8000…30（快门，秒）, exposure_mode:ambient|manual（物理天空下的曝光模式：ambient 环境测光，按场景的环境光测光、不看画面；写入时省略为 ambient）, exposure_compensation:-5…5（环境测光的补偿，档，正值变亮）；iso 和 shutter_s 只在物理天空下手动曝光时生效}，均可省略（默认 35、[36,24]、3:2、0.1、1000、空、false、2.8、10、\"\"、100、1/4000、ambient、0）；" +
				"视场角按画面长边对应传感器长边：同一焦距下 2:3 竖幅和 3:2 横幅拍到的范围一样大，只是方向不同；depth_of_field 打开后白模导出按光圈和对焦虚化（线稿不虚化）。" +
				"空物体是没有子物件的 group，可以当对焦目标或父级。" +
				"add_objects 和 update_objects 的条目可写 look_at:[x,y,z]（世界坐标）代替 transform.rotation：摄像机和灯光的 −Z、其余物件的 +Z 对准该点（同 three.js Object3D.lookAt），update 时 transform 可只写 position 或省略；父级在同一次修改里也被移动时会被拒绝，分两次提交。" +
				"update_objects{updates:[{object_id, 只写要改的字段}]}（primitive.params、light、camera 按键合并，只改写到的字段；light 改 type 时去掉新类型没有的字段；material_slots 按槽位合并，写 null 清除该槽位）；remove_objects{object_ids}（连同子物件）；" +
				"reparent_objects{moves:[{object_id, parent_id(分组或 null), order?}], create_groups?:[{id, name, parent_id, order?, transform?}], remove_groups?:[分组 ID]}：编组、取消编组和改挂，按新建→改挂→删除执行；" +
				"改挂自动保持物件的世界位置，不要写 transform；省略 order 排到父级末尾；新分组省略 transform 时放在挂进它的物件的中点；删除的分组要先把子物件挂走；add_objects、remove_objects、reparent_objects 都改物件列表，一次修改里最多一个；" +
				"set_environment{environment:{sky:{kind:procedural, sun_elevation_deg（0 地平线、90 天顶）, sun_azimuth_deg（0 朝 +Z、90 朝 +X）, turbidity}|{kind:physical, …见下}|{kind:color, color}（暂无内置 HDRI）, ambient:{color, intensity:0…10}, fog:{color, near, far}（three.js Fog，米）或 null}}（三项都要写；环境只作用于 lit）；" +
				"set_render{render:{mode?:clay|lit, exposure_ev?:-10…10（物理天空以外的曝光补偿）, exposure_mode?:ambient|manual（物理天空下临时视点和预设取景的曝光模式，默认 ambient 环境测光）, exposure_compensation?:-5…5（它们环境测光的补偿）, exposure_ev100?:-10…20（它们手动曝光时的 EV100）, engine?:workbench|realtime_gi, resolution?:1k|2k|4k|8k}}（按键合并）；" +
				"set_instance_model{object_id, model{content_revision_id}, scope:this|all_instances, object_ids}。" +
				"物理天空（真实的大气和太阳，调时间产生真实的光照变化）sky:{kind:physical, sun_mode:location（按地点和时间推算太阳）|manual（用 sun_elevation_deg、sun_azimuth_deg）, latitude_deg:-90…90, longitude_deg:-180…180（东经为正）, " +
				"timezone（IANA 时区名，大小写与时区库一致，如 Asia/Shanghai、Europe/London；夏令时由时区处理）, place（显示用地名，≤80 字）, date:\"YYYY-MM-DD\"（必填）, local_time:\"HH:MM\"（当地时刻）, " +
				"north_azimuth_deg:0…360（场景里正北的方位，与 sun_azimuth_deg 同一约定，默认 180 即 −Z 为北）, sun_elevation_deg:-10…90, sun_azimuth_deg（手动模式用）, altitude_m:0…20000（海拔，米）, air_density:0…10, dust_density:0…50, ozone_density:0…10, " +
				"sun_disc:bool, sun_size_deg:0.1…10（太阳盘角直径，默认 0.545）, sun_strength:0…10（太阳亮度倍率）, sun_temperature_k:2000…12000（色温，6500 为原色，越低越暖）, " +
				"night:{moon:bool, stars:bool（含无月晴夜天空本身的夜天光）, milky_way:bool, moon_strength:0…10, moon_temperature_k:2000…12000, stars_strength:0…10（星星、银河、夜天光）, stars_temperature_k:2000…12000, night_look:0…1（夜色：入夜后画面偏蓝、降饱和，0 为物理颜色）}, " +
				"aerial_perspective:{enabled:bool, distance_scale:0.1…1000}}：" +
				"地点三项（latitude_deg、longitude_deg、timezone）一起写或都不写，date 必填，其余省略取默认（上海 31.2304、121.4737，12:00，晴朗，太阳盘开，亮度 1，6500 K）；不能带 turbidity、color。" +
				"大气预设（写四个字段）：晴朗 altitude_m 0、air_density 1、dust_density 1、ozone_density 1；雾霾 dust_density 12（其余同晴朗）；高原 altitude_m 4000、dust_density 0.2；沙尘 dust_density 40。" +
				"夜空只在 sun_mode:location 下画：月亮按真实位置、月相和月光，星星、银河随地点和时刻转动；手动模式没有月亮和星空，night_look 照常生效；night 省略或少写的项取默认（全开、亮度 1、6500 K、夜色 1）。" +
				"空气透视让远处变蓝变淡，天空背景不受影响；distance_scale 乘到距离上，院子、街道调到 50–200 才看得出；默认开启、倍率 1。" +
				"物理天空下天空光由大气算出，环境光不生效；太阳、天空和灯光都是物理单位（晴天正午约 10 万 lux，灯光坎德拉），雾按它所在处的照度着色。" +
				"太阳参考：物理天空的地点模式下，read_scene 和写了 set_environment 的 edit_scene 结果带 sun：elevation_deg（当前视高度，含折射）、compass_deg（罗盘方位，正北 0° 顺时针）、" +
				"scene_azimuth_deg（换成场景方位，与 sun_azimuth_deg 同一约定，已计入 north_azimuth_deg）、solar_noon、noon_elevation_deg、sunrise、sunset（太阳上缘过地平线）、polar（day 极昼、night 极夜）、" +
				"morning 和 evening 里的 golden_hour 黄金时刻（太阳高度 −4°…6°）和 blue_hour 蓝调时刻（−6°…−4°），都是 [起, 止]；时刻都是场景时区的 HH:MM，当天没有的项省略。" +
				"要黄金时刻、蓝调时刻或某个太阳高度时，按它选 local_time；摆逆光（摄像机朝向太阳）、侧光机位时用 scene_azimuth_deg。观感仍用 render_view 的 lit 确认。" +
				"月亮参考：同样的情况下结果带 moon：phase（new、waxing_crescent、first_quarter、waxing_gibbous、full、waning_gibbous、last_quarter、waning_crescent）、illumination（照亮比例 0…1）、elevation_deg、compass_deg、scene_azimuth_deg、moonrise、moonset，" +
				"以及 previous_full_moon、next_full_moon、previous_new_moon、next_new_moon（当地日期）；「满月之夜」「新月看银河」先按它选 date，再写 local_time，月亮在天上时 elevation_deg > 0。" +
				"资产库里的模型：search_assets 按 kind:model 查到后，用它的 content_revision_id 写 add_objects 的 model{content_revision_id}，并把 model 要点里的 materials 原样写进 material_slots（资产带的材质）；可放多个实例，都引用同一版模型；size_m 与 mount 见搜索结果的 model 要点。" +
				"渲染方式：clay 是素模，用固定的工作室光（光从视角左上方照来，不投影，带环境光遮蔽，背景是中性灰），不受天空、太阳、环境光、雾和灯光物件影响，看几何、比例和构图；lit 是最终渲染：材质、灯光物件、天空当环境光照和反射、太阳、环境光、雾、阴影和环境光遮蔽，按 render.exposure_ev 曝光（档，只作用于 lit；物理天空下改为摄像机的环境测光或手动曝光，见建议曝光）。" +
				"渲染器 render.engine 只作用于材质光照（lit 的导出与回看；白模和线稿总按工作台画）：workbench 是默认的工作台渲染；realtime_gi 是实时 GI，加入屏幕空间的间接光（墙面互相渗色）、反射和接触阴影，画面更接近成片，但回看和导出要多帧累积、更慢；path_tracing 还不能选。" +
				"输出分辨率 render.resolution 只决定 export_shots 导出图的长边（1k 1024、2k 2048、4k 4096、8k 8192），不影响画面内容和回看；8K 实时 GI 的材质光照一张要一分钟以上，设备画不了时导出失败，可以换低一档。" +
				"material_slots{槽位:{preset, params}}：基础体只有 main，模型实例的槽位见 read_scene 的 model.slots，换模型后另行提交；没写的槽位按模型的材质提示或默认材质显示；params 省略的取默认。" +
				"材质（数值未注明范围的为 0–1，颜色 #rrggbb）：" + creativescene.MaterialCatalog() + "。" +
				"打光：室外由天空的太阳和天空光照亮；台灯、壁灯用点光 20–80 cd，舞台聚光 100–300 cd，平行光补光 0.3–1（程序天空的太阳约 2.2）；投射阴影的灯光不超过 4 盏，太多会卡。物理天空下灯光同样用坎德拉，白天几乎看不出，入夜后才显出来。" +
				"建议曝光：物理天空下摄像机和临时视点默认环境测光：按场景的环境光测光（不看画面，不随取景变），入夜后平滑变暗，只比正午暗 2–4 档（满月之夜约 2.5 档，新月之夜约 3.5 档，天空深蓝、看得见银河），入夜不必改曝光；" +
				"要更亮或更暗写 exposure_compensation；测光看的是场景原点的天光，室内或浓荫下的取景偏暗，补偿 +1～+2。" +
				"要按真实相机曝光时写 exposure_mode:manual，给摄像机设 iso 和 shutter_s（f_stop 同时决定景深），EV100 = log2(f_stop²/shutter_s) − log2(iso/100)，EV100 越大画面越暗；" +
				"参考值：晴天 15、阴天 12–13、日落 9–12、蓝调时刻 5–8、城市夜景 3–5、月光 −3、银河 −7；render.exposure_ev100 只管临时视点和预设取景的手动曝光，不影响摄像机。" +
				"其他天空用 exposure_ev：太阳高于 30° 为 0，15°–30° 为 +1，5°–15° 为 +2，日落前后 +2～+3；只有灯光的室内 0～+1。配好材质、打好光后用 render_view 的 mode:lit 回看，过暗或过曝调曝光。" +
				"撤回改动：单独一项 {op:\"revert_run\", run_id:\"previous\"(本会话上次改过本场景的运行)|运行 ID, object_ids?}，不需要基线，之后被别人改过的跳过并列出。" +
				"摆好摄像机后先用 render_view 按摄像机回看确认构图，再用 export_shots 导出。新模型用 write_model。基础体（原点在底面中心）：" + creativescene.BuiltinCatalog(),
			ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
				"document_id":       doc,
				"based_on_revision": base,
				"operations": {Type: schema.Array, Required: true, Desc: "1 到 50 项", ElemInfo: &schema.ParameterInfo{Type: schema.Object, SubParams: map[string]*schema.ParameterInfo{
					"op": {Type: schema.String, Required: true, Enum: []string{"add_objects", "update_objects", "remove_objects", "reparent_objects", "set_environment", "set_render", "set_instance_model", "revert_run"}},
				}}},
			}),
		}, nil
	case writeModelTool:
		return &schema.ToolInfo{
			Name: writeModelTool,
			Desc: "用代码生成模型并放进场景：新增物件，或替换模型实例的模型。代码在创作者的页面编译，调用等到写入后返回尺寸 size_m、三角面、材质槽、部件和挂点；编译失败返回错误和行号，改代码重试。" +
				"代码是只有 export 的 ES 模块：export const params = {h: {type:'number', default:6, min:2, max:20, unit:'m', label:'高度'}}（number|integer|boolean|enum(options)|color，≤32 项）；" +
				"export const mount = 'floor'（floor 原点在底面中心，wall 背面底边中心，ceiling 顶面中心，free 几何中心）；export default function build({params, THREE, lib, rng}) {…; return group}。" +
				sceneConventions + "只能是三角网格；材质只用 lib.material(slot, hint)（hint 如 stone、wood），不用纹理和着色器。lib：subtract/union/intersect(a,b)、extrude(shape,depth,{bevel})、lathe(points,segments)、array(obj,count,[x,y,z])、radial(obj,count,radius,axis)；rng() 是确定性随机数。" +
				"对象名 part:<id> 是部件，空对象 anchor:<id> 是挂点。代码 ≤64 KiB，编译 ≤5 秒，三角面 ≤30 万。" +
				"只改参数时给 base_content_revision_id 代替 code。被多个实例共用的模型：scope=this 只改这一个（分出新模型），all_instances 一起改。" +
				"烘焙期间目标被改过时返回 conflict 和 retry_job_id：单独传它、新基线和 target 即可写入，不重新编译。",
			ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
				"document_id":       doc,
				"based_on_revision": base,
				"target": {Type: schema.Object, Desc: "add 与 replace 二选一", SubParams: map[string]*schema.ParameterInfo{
					"add": {Type: schema.Object, SubParams: map[string]*schema.ParameterInfo{
						"name":      str(""),
						"transform": {Type: schema.Object, Required: true, SubParams: map[string]*schema.ParameterInfo{"position": numbers, "rotation": numbers, "scale": numbers}},
						"parent_id": str("编组 ID，可省略"),
					}},
					"replace": {Type: schema.Object, SubParams: map[string]*schema.ParameterInfo{
						"object_id": {Type: schema.String, Required: true},
						"scope":     {Type: schema.String, Enum: []string{"this", "all_instances"}},
					}},
				}},
				"code":                     str("与 base_content_revision_id 二选一"),
				"base_content_revision_id": str("沿用这个模型修订的代码"),
				"params":                   {Type: schema.Object, Desc: "参数取值，省略的用默认值"},
				"name_hint":                str("模型名，如「哥特式石柱」"),
				"retry_job_id":             str("冲突结果给出的任务 ID；不能和 code、params 同用"),
			}),
		}, nil
	case exportShotsTool:
		return &schema.ToolInfo{
			Name: exportShotsTool,
			Desc: "把场景里的摄像机导出成图，放到当前画布上这个场景节点的右侧：每台摄像机一行，clay（白模：固定的工作室光，不受环境和灯光影响，打开景深的摄像机按光圈虚化）、line（线稿）和 lit（材质光照：与 render_view 的 lit 同一套渲染，景深同 clay）各一列，节点记下来源修订和镜头（lens：焦距、景深开关、光圈、对焦距离和对焦物件），场景之后改了会提示过时。" +
				"图在创作者的页面渲染，材质光照按场景的渲染器，白模和线稿按工作台；长边按 render.resolution（缺省 2k 即 2048）；4K、8K 的白模和材质光照是 JPEG，线稿是 PNG。调用等到图放上画布后返回节点 ID，不返回图片；要看画面先用 render_view。场景须已在当前画布上。",
			ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
				"document_id":       doc,
				"camera_object_ids": strs("要导出的摄像机，省略为全部摄像机（按物件顺序），最多 32 台"),
				"passes":            {Type: schema.Array, Desc: "导出方式，省略为 clay 和 line；配了材质和灯光时加 lit", ElemInfo: &schema.ParameterInfo{Type: schema.String, Enum: []string{"clay", "line", "lit"}}},
			}),
		}, nil
	default:
		return &schema.ToolInfo{
			Name: renderViewTool,
			Desc: "给场景拍一张截图交给你看：默认 mode:clay 素模（固定的工作室光，不受环境和灯光影响），检查摆放、比例和构图；mode:lit 按最终渲染（材质、灯光、天空、环境光、雾、曝光）出图，检查材质和打光；物理天空下摄像机按自己的曝光（环境测光，或 iso、shutter_s、f_stop 手动），临时视点和预设取景按 render 的曝光设置（默认环境测光），所以它们的亮度可能和摄像机拍出来的不同，手动曝光的摄像机尤其如此：判断最终画面的明暗要按那台摄像机回看；结果的 exposure 写着这张截图怎么曝光（by:camera|default_camera，mode，环境测光的 compensation 或手动的 ev100）。三选一：场景里的摄像机、临时视点，或预设取景（overview 斜上方总览、top 俯视、front 正视、side 侧视，按可见物件自动取景）。" +
				"截图与编辑器视口的白模或最终渲染、对应的导出是同一套画面（带环境光遮蔽）。默认全清晰，先看清结构；要检查焦点和虚化时按摄像机回看并带 depth_of_field:true，按光圈和对焦虚化，这台摄像机须已打开 depth_of_field。" +
				"截图 mode:lit 按场景的渲染器（render.engine）画，素模按工作台；长边 1024；实时 GI 要多帧累积，比工作台慢。截图在创作者的页面渲染，调用等到完成后返回；scene_revision 是截图画的修订，不能当基线。",
			ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
				"document_id":      doc,
				"camera_object_id": str("摄像机物件 ID"),
				"depth_of_field":   {Type: schema.Boolean, Desc: "按这台摄像机的光圈和对焦虚化；只能与 camera_object_id 一起用"},
				"viewpoint": {Type: schema.Object, Desc: "临时视点，画幅 4:3", SubParams: map[string]*schema.ParameterInfo{
					"position": numbers, "target": numbers,
					"focal_mm": {Type: schema.Number, Required: true, Desc: "8 到 800"},
				}},
				"preset": {Type: schema.String, Enum: []string{"overview", "top", "front", "side"}},
				"mode":   {Type: schema.String, Enum: []string{"clay", "lit"}, Desc: "渲染方式，省略为 clay"},
			}),
		}, nil
	}
}

func (t *sceneTool) InvokableRun(ctx context.Context, raw string, _ ...tool.Option) (string, error) {
	h := t.handler
	step := toolStepFrom(ctx)
	if step == "" {
		return "", ErrRunState
	}
	var out any
	var err error
	switch t.name {
	case readSceneTool:
		out, err = h.readScene(ctx, raw)
	case editSceneTool:
		out, err = h.editScene(ctx, step, raw)
	case writeModelTool:
		out, err = h.writeModel(ctx, step, raw)
	case exportShotsTool:
		out, err = h.exportShots(ctx, step, raw)
	default:
		out, err = h.renderView(ctx, step, raw)
	}
	if err != nil {
		if errors.Is(err, creativescene.ErrUnreadable) {
			// The stored scene is at fault, not the call: its detail goes to the log.
			h.service.logger.ErrorContext(ctx, "creative scene unreadable",
				slog.String("run_id", h.held.RunID), slog.String("step_id", step), slog.String("tool", t.name), slog.String("error", err.Error()))
		}
		return "", sceneRefusal(err)
	}
	result, err := json.Marshal(out)
	if err != nil {
		return "", err
	}
	// A conflict is an answer with structure, but the call did not do what it
	// was asked: the step fails with the conflict as its result.
	var status struct {
		Status string `json:"status"`
	}
	if json.Unmarshal(result, &status) == nil && status.Status == "conflict" {
		return "", &structuredRefusal{result: string(result)}
	}
	return string(result), nil
}

// sceneRefusal words what the model can act on; a parked call passes through.
func sceneRefusal(err error) error {
	var parked errParked
	var public creativeops.Public
	switch {
	case errors.As(err, &parked):
		return err
	case errors.Is(err, creativescene.ErrUnreadable):
		// Before any explanation it carries: rewriting the call cannot help.
		return refuseTool("这个场景存着的数据当前服务读不出来（多半是更新版本的服务写入了它不认识的内容），不是这次调用写错了。" +
			"不要改写参数重试，也不要换别的场景工具绕开；告诉创作者这个场景暂时无法读写、原因已记录，稍后再试")
	case errors.Is(err, creativedocument.ErrTrashed):
		return refuseTool("场景在回收站里，恢复后才能读写")
	case errors.Is(err, creativedocument.ErrNotFound):
		return refuseTool("场景或物件不存在，或不属于这个账号；请核对 document_id")
	case errors.Is(err, creativeops.ErrValidation) && !errors.As(err, &public) && !errors.Is(err, errToolArguments):
		// asRefusal would word it as a generation request's references.
		return refuseTool("场景操作的字段或取值不符合工具说明，请对照说明重写这次调用")
	}
	return asRefusal(err)
}

// sceneConsentInTx is the scene's share of the run's consent: scenes are
// canvas metadata, read and sent only when the canvas may leave the account.
func sceneConsentInTx(ctx context.Context, tx store.TxAccountScope, consentID string) error {
	consent, err := readConsentForUpdate(ctx, tx, consentID)
	if err != nil {
		return err
	}
	if !consentHas(consent, "canvas_metadata") {
		return ErrEgressRequired
	}
	return nil
}

func (h *runtimeHandler) readScene(ctx context.Context, raw string) (any, error) {
	var args struct {
		DocumentID  string   `json:"document_id"`
		ObjectIDs   []string `json:"object_ids"`
		IncludeCode []string `json:"include_code"`
	}
	if err := strictArguments(raw, &args); err != nil {
		return nil, err
	}
	var out creativescene.SceneRead
	err := h.scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		if err := sceneConsentInTx(ctx, tx, h.held.ConsentID); err != nil {
			return err
		}
		runs, err := conversationRunsInTx(ctx, tx, h.held.ConversationID)
		if err != nil {
			return err
		}
		out, err = creativescene.AgentReadInTx(ctx, tx, creativescene.ReadInput{DocumentID: args.DocumentID, ObjectIDs: args.ObjectIDs,
			IncludeCode: args.IncludeCode, ConversationRuns: runs, MaxBytes: h.service.limits.ToolResultBytes - 1024})
		return err
	})
	return out, err
}

// sceneWrite runs one scene write under an operation derived from the call,
// fenced by the run's admission: a call recovered after its write committed
// finds the receipt and returns what it answered then.
func (h *runtimeHandler) sceneWrite(ctx context.Context, key, step, raw string, apply func(context.Context, store.TxAccountScope) (any, error)) (any, error) {
	operation := toolOperation(h.held.RunID + "/" + step)
	executor := creativeops.Executor{}
	if receipt, err := executor.Lookup(ctx, h.scope, operation); err == nil {
		return json.RawMessage(receipt.Outcome.Response), nil
	} else if !errors.Is(err, creativeops.ErrNotFound) {
		return nil, err
	}
	receipt, err := executor.Run(ctx, h.scope, creativeops.Operation{Key: key, Capability: "agent_start",
		Validate: func(json.RawMessage) error { return nil },
		Apply: func(ctx context.Context, tx store.TxAccountScope, _ json.RawMessage) (creativeops.Outcome, error) {
			if err := sceneConsentInTx(ctx, tx, h.held.ConsentID); err != nil {
				return creativeops.Outcome{}, err
			}
			out, err := apply(ctx, tx)
			if err != nil {
				return creativeops.Outcome{}, err
			}
			response, err := json.Marshal(out)
			if err != nil {
				return creativeops.Outcome{}, err
			}
			return creativeops.Outcome{HTTPStatus: 200, Response: response, ResultKind: "scene_tool"}, nil
		}},
		creativeops.Command{OperationID: operation, CreatedAt: time.Now().UTC(), Payload: json.RawMessage(raw),
			Guard: func(ctx context.Context, tx store.TxAccountScope, _ string, _ creativeops.Command) error {
				return h.service.admitRunInTx(ctx, tx, h.held)
			}})
	if err != nil {
		return nil, err
	}
	return json.RawMessage(receipt.Outcome.Response), nil
}

// revisionArg is a revision the model passes back: an integer, or the
// decimal string other results print.
type revisionArg int64

func (r *revisionArg) UnmarshalJSON(raw []byte) error {
	var n json.Number
	if err := json.Unmarshal(raw, &n); err != nil {
		var s string
		if err := json.Unmarshal(raw, &s); err != nil {
			return err
		}
		n = json.Number(s)
	}
	v, err := n.Int64()
	*r = revisionArg(v)
	return err
}

func (h *runtimeHandler) editScene(ctx context.Context, step, raw string) (any, error) {
	var args struct {
		DocumentID string            `json:"document_id"`
		BasedOn    revisionArg       `json:"based_on_revision"`
		Operations []json.RawMessage `json:"operations"`
	}
	if err := strictArguments(raw, &args); err != nil {
		return nil, err
	}
	if len(args.Operations) == 0 || len(args.Operations) > 50 {
		return nil, refuseTool("operations 需要 1 到 50 项")
	}
	return h.sceneWrite(ctx, "agent.edit_scene", step, raw, func(ctx context.Context, tx store.TxAccountScope) (any, error) {
		runs, err := conversationRunsInTx(ctx, tx, h.held.ConversationID)
		if err != nil {
			return nil, err
		}
		return creativescene.AgentEditInTx(ctx, tx, creativescene.EditInput{DocumentID: args.DocumentID, RunID: h.held.RunID,
			BasedOn: int64(args.BasedOn), Operations: args.Operations, ConversationRuns: runs})
	})
}

type writeModelArgs struct {
	DocumentID string      `json:"document_id"`
	BasedOn    revisionArg `json:"based_on_revision"`
	Target     struct {
		Add     *creativescene.AddTarget     `json:"add"`
		Replace *creativescene.ReplaceTarget `json:"replace"`
	} `json:"target"`
	Code                  string         `json:"code"`
	BaseContentRevisionID string         `json:"base_content_revision_id"`
	Params                map[string]any `json:"params"`
	NameHint              string         `json:"name_hint"`
	RetryJobID            string         `json:"retry_job_id"`
}

func (h *runtimeHandler) writeModel(ctx context.Context, step, raw string) (any, error) {
	var args writeModelArgs
	if err := strictArguments(raw, &args); err != nil {
		return nil, err
	}
	if args.RetryJobID != "" {
		if args.Code != "" || args.BaseContentRevisionID != "" || args.Params != nil {
			return nil, refuseTool("retry_job_id 不能和 code、params、base_content_revision_id 一起使用：重试沿用那次烘焙的产物")
		}
		return h.sceneWrite(ctx, "agent.retry_bake", step, raw, func(ctx context.Context, tx store.TxAccountScope) (any, error) {
			runs, err := conversationRunsInTx(ctx, tx, h.held.ConversationID)
			if err != nil {
				return nil, err
			}
			bake, conflict, err := creativescene.RetryBakeInTx(ctx, tx, creativescene.RetryInput{DocumentID: args.DocumentID, RunID: h.held.RunID,
				ConversationRuns: runs, RetryJobID: args.RetryJobID, BasedOn: int64(args.BasedOn), Add: args.Target.Add, Replace: args.Target.Replace})
			if err != nil || conflict != nil {
				return conflict, err
			}
			return bakeSucceeded(ctx, tx, *bake)
		})
	}
	return h.parkOnJob(ctx, func(ctx context.Context, tx store.TxAccountScope) (creativescene.Job, any, error) {
		job, conflict, err := creativescene.CreateBakeJobInTx(ctx, tx, creativescene.BakeInput{DocumentID: args.DocumentID, RunID: h.held.RunID, ToolCallID: step,
			BasedOn: int64(args.BasedOn), Add: args.Target.Add, Replace: args.Target.Replace, Params: args.Params, Code: args.Code,
			BaseContentRevisionID: args.BaseContentRevisionID, NameHint: args.NameHint})
		if conflict != nil {
			return job, conflict, err
		}
		return job, nil, err
	}, raw)
}

func (h *runtimeHandler) renderView(ctx context.Context, step, raw string) (any, error) {
	var args struct {
		DocumentID     string                   `json:"document_id"`
		CameraObjectID string                   `json:"camera_object_id"`
		Viewpoint      *creativescene.Viewpoint `json:"viewpoint"`
		Preset         string                   `json:"preset"`
		DepthOfField   bool                     `json:"depth_of_field"`
		Mode           string                   `json:"mode"`
	}
	if err := strictArguments(raw, &args); err != nil {
		return nil, err
	}
	return h.parkOnJob(ctx, func(ctx context.Context, tx store.TxAccountScope) (creativescene.Job, any, error) {
		job, err := creativescene.CreateRenderJobInTx(ctx, tx, creativescene.RenderInput{DocumentID: args.DocumentID, RunID: h.held.RunID, ToolCallID: step,
			CameraObjectID: args.CameraObjectID, Viewpoint: args.Viewpoint, Preset: args.Preset, Mode: args.Mode, DepthOfField: args.DepthOfField})
		return job, nil, err
	}, raw)
}

// exportShots places the scene's camera images on the run's canvas, beside
// the scene node; the nodes are made when the page completes the job.
func (h *runtimeHandler) exportShots(ctx context.Context, step, raw string) (any, error) {
	var args struct {
		DocumentID      string   `json:"document_id"`
		CameraObjectIDs []string `json:"camera_object_ids"`
		Passes          []string `json:"passes"`
	}
	if err := strictArguments(raw, &args); err != nil {
		return nil, err
	}
	return h.parkOnJob(ctx, func(ctx context.Context, tx store.TxAccountScope) (creativescene.Job, any, error) {
		var canvasID *string
		if err := tx.QueryRow(ctx, "creative_agent_conversations", "canvas_id", "id=$2", h.held.ConversationID).Scan(&canvasID); err != nil {
			return creativescene.Job{}, nil, err
		}
		in := creativescene.ExportJobInput{DocumentID: args.DocumentID, RunID: h.held.RunID, ToolCallID: step, CameraObjectIDs: args.CameraObjectIDs, Passes: args.Passes}
		if canvasID != nil {
			in.CanvasID = *canvasID
		}
		job, err := creativescene.CreateExportJobInTx(ctx, tx, in)
		return job, nil, err
	}, raw)
}

// parkOnJob creates the call's client job behind the run's admission and
// parks on it. An answer that needs no job (a conflict) comes back as is. A
// job that already ended (a rerun after its result was written elsewhere)
// answers with its result.
func (h *runtimeHandler) parkOnJob(ctx context.Context, create func(context.Context, store.TxAccountScope) (creativescene.Job, any, error), args string) (any, error) {
	var job creativescene.Job
	var answer any
	err := h.scope.WithTxScope(ctx, func(tx store.TxAccountScope) error {
		if err := h.service.admitRunInTx(ctx, tx, h.held); err != nil {
			return err
		}
		if err := sceneConsentInTx(ctx, tx, h.held.ConsentID); err != nil {
			return err
		}
		var err error
		if job, answer, err = create(ctx, tx); err != nil || answer != nil {
			return err
		}
		if !creativescene.Terminal(job.State) {
			return nil
		}
		res, err := creativescene.ResultOf(job)
		if err != nil {
			return err
		}
		result, _, err := sceneToolResult(ctx, tx, job, res, args)
		answer = json.RawMessage(result)
		return err
	})
	if err != nil || answer != nil {
		return answer, err
	}
	return nil, errParked{jobID: job.ID}
}

// bakeSucceeded is write_model's answer for a written model.
func bakeSucceeded(ctx context.Context, tx store.TxAccountScope, bake creativescene.BakeResult) (any, error) {
	model, err := creativescene.BakedModelInTx(ctx, tx, bake.ContentRevisionID)
	if err != nil {
		return nil, err
	}
	return struct {
		Status            string   `json:"status"`
		ContentRevisionID string   `json:"content_revision_id"`
		ObjectIDs         []string `json:"object_ids"`
		WrittenRevision   int64    `json:"written_revision"`
		creativescene.BakedModel
	}{"succeeded", bake.ContentRevisionID, bake.ObjectIDs, int64(bake.Revision), model}, nil
}

// renderSucceeded is render_view's answer: the screenshot, bound to the next
// request as an image, the revision it shows and, lit under a physical sky,
// how it was exposed.
func renderSucceeded(ctx context.Context, tx store.TxAccountScope, job creativescene.Job, render creativescene.RenderResult) (any, error) {
	out := map[string]any{"image_revision_id": render.ContentRevisionID, "content_revision_id": render.ContentRevisionID, "scene_revision": int64(render.SceneRevision)}
	exposure, err := creativescene.RenderExposureInTx(ctx, tx, job, render.SceneRevision)
	if err != nil {
		return nil, err
	}
	if exposure != nil {
		out["exposure"] = exposure
	}
	batch, err := creativecontent.ReadDisplayBatchInTx(ctx, tx, []string{render.ContentRevisionID})
	if err != nil {
		return nil, err
	}
	for _, m := range batch[render.ContentRevisionID].Revision.Media {
		if m.Role == "original" && m.Width != nil && m.Height != nil {
			out["width"], out["height"] = *m.Width, *m.Height
		}
	}
	return out, nil
}

// exportSucceeded is export_shots' answer: where the images went, no images
// (the model looks with render_view).
func exportSucceeded(export creativescene.ExportResult) any {
	type node struct {
		CameraObjectID string                         `json:"camera_object_id"`
		Pass           string                         `json:"pass"`
		NodeID         string                         `json:"node_id"`
		Lens           creativecanvas.SceneExportLens `json:"lens"`
	}
	nodes := make([]node, 0, len(export.Nodes))
	for _, n := range export.Nodes {
		nodes = append(nodes, node{n.CameraObjectID, n.Pass, n.NodeID, n.Lens})
	}
	return map[string]any{"status": "succeeded", "scene_revision": int64(export.SceneRevision), "change_id": export.ChangeID, "nodes": nodes}
}

// sceneToolResult is what the model reads for a finished job. failed marks a
// call that did not do what was asked. args are the parked call's arguments,
// whose baseline a conflict is worded from.
func sceneToolResult(ctx context.Context, tx store.TxAccountScope, job creativescene.Job, res creativescene.JobResult, args string) (string, bool, error) {
	var out any
	var err error
	failed := false
	switch {
	case res.Bake != nil:
		out, err = bakeSucceeded(ctx, tx, *res.Bake)
	case res.Render != nil:
		out, err = renderSucceeded(ctx, tx, job, *res.Render)
	case res.Export != nil:
		out = exportSucceeded(*res.Export)
	case res.Failure != nil:
		failed = true
		switch res.Failure.Code {
		case creativescene.JobExpired:
			out = map[string]any{"status": "expired", "message": "创作者的页面一直没有打开，任务已过期。请告诉创作者打开创意空间页面后再继续。"}
		case creativescene.JobCancelled:
			out = map[string]any{"status": "cancelled", "message": res.Failure.Message}
		case "conflict":
			var base struct {
				BasedOn revisionArg `json:"based_on_revision"`
			}
			_ = json.Unmarshal([]byte(args), &base)
			out, err = creativescene.JobConflictInTx(ctx, tx, job, *res.Failure, int64(base.BasedOn))
		default:
			message := res.Failure.Message
			if res.Failure.Code == "host_error" {
				message = strings.TrimSpace(message + " 问题出在页面上，可以稍后重试。")
			}
			out = map[string]any{"status": "failed", "error": map[string]string{"code": res.Failure.Code, "message": message}}
		}
	default:
		return "", false, errors.New("creative agent: empty job result")
	}
	if err != nil {
		return "", false, fmt.Errorf("creative agent: scene job %s result: %w", job.ID, err)
	}
	raw, err := json.Marshal(out)
	return string(raw), failed, err
}

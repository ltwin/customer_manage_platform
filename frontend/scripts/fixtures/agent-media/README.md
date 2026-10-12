聊天媒体浏览器回归使用的合成样本，不包含真实拍摄内容：

- image.png：1280 × 720 纯色图片。
- video.webm：3 秒、320 × 180 的 VP8 纯色视频。
- audio.wav：3 秒、8 kHz 单声道静音音频。

测试通过 Playwright route 提供这些文件，不依赖外部媒体服务。

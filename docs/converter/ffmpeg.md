ffmpeg -hide_banner -y -i input.mkv \
  -map_metadata -1 -map_chapters -1 \
  -filter_complex "
    [0:v]split=3[v720][v480][v360];
    [v720]scale=-2:720:flags=bicubic[v720o];
    [v480]scale=-2:480:flags=bicubic[v480o];
    [v360]scale=-2:360:flags=bicubic[v360o]
  " \
  -map [v720o] -map 0:a:0 \
    -c:v:0 libx264 -preset faster -profile:v:0 high -level:v:0 4.0 \
    -pix_fmt:v:0 yuv420p -sc_threshold:v:0 0 -x264-params:v:0 rc-lookahead=10 \
    -b:v:0 1050k -maxrate:v:0 1155k -bufsize:v:0 2300k -g:v:0 {gop} -keyint_min:v:0 {gop} \
    -c:a:0 aac -b:a:0 80k -ar:a:0 48000 -ac:a:0 2 \
  -map [v480o] -map 0:a:0 \
    -c:v:1 libx264 -preset faster -profile:v:1 high -level:v:1 4.0 \
    -pix_fmt:v:1 yuv420p -sc_threshold:v:1 0 -x264-params:v:1 rc-lookahead=10 \
    -b:v:1 700k -maxrate:v:1 770k -bufsize:v:1 1500k -g:v:1 {gop} -keyint_min:v:1 {gop} \
    -c:a:1 aac -b:a:1 80k -ar:a:1 48000 -ac:a:1 2 \
  -map [v360o] -map 0:a:0 \
    -c:v:2 libx264 -preset faster -profile:v:2 high -level:v:2 4.0 \
    -pix_fmt:v:2 yuv420p -sc_threshold:v:2 0 -x264-params:v:2 rc-lookahead=10 \
    -b:v:2 320k -maxrate:v:2 352k -bufsize:v:2 700k -g:v:2 {gop} -keyint_min:v:2 {gop} \
    -c:a:2 aac -b:a:2 80k -ar:a:2 48000 -ac:a:2 2 \
  -f hls -hls_time 4 -hls_playlist_type vod -hls_list_size 0 \
  -hls_flags independent_segments \
  -master_pl_name master.m3u8 \
  -var_stream_map "v:0,a:0,name:720 v:1,a:1,name:480 v:2,a:2,name:360" \
  -hls_segment_filename "outputDir/%v/seg%03d.ts" \
  outputDir/%v/index.m3u8

# {gop} = round(hls_time * source_fps)  — вычисляется динамически из ffprobe
# например: 25 fps → gop=100, 24 fps → gop=96, 30 fps → gop=120
#
# Аудио маппится per-stream (3 раза) — HLS-мультиплексор не поддерживает shared a:0.
# bufsize ~2× maxrate — запас для энкодера на сценах с высокой детализацией.
# :v:N specifiers на x264-params и sc_threshold — изолируют опции от аудио-потоков.

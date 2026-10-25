// Licensed to the Apache Software Foundation (ASF) under one
// or more contributor license agreements. See the NOTICE file
// distributed with this work for additional information regarding
// copyright ownership. The ASF licenses this file to You under the
// Apache License, Version 2.0 (the "License"); you may not use this
// file except in compliance with the License. You may obtain a copy
// at http://www.apache.org/licenses/LICENSE-2.0
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.

package video

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCreativeSelectionAndDashFiltering(t *testing.T) {
	body := []byte(`{"aweme_detail":{"aweme_id":"123456789","desc":"mountains","video":{"duration":13000,"play_addr":{"url_list":["https://v5.zjcdn.com/default"],"width":720,"height":1280},"bit_rate":[{"format":"dash","gear_name":"dash_1080_0","play_addr":{"url_list":["https://v5.zjcdn.com/dash"],"data_size":1,"width":1080,"height":1920}},{"format":"mp4","gear_name":"1080_1_1","play_addr":{"url_list":["https://v5.zjcdn.com/hevc"],"data_size":100,"width":1080,"height":1920}},{"format":"mp4","gear_name":"normal_1080_0","play_addr":{"url_list":["https://v5.zjcdn.com/h264"],"data_size":200,"width":1080,"height":1920}}]}}}`)
	s, ok := ParseDouyin(body, "123456789")
	require.True(t, ok)
	require.Len(t, s.Variants, 2)
	play, _, ok := PickCreative(s, 1080)
	require.True(t, ok)
	require.Equal(t, "normal_1080_0", play.Name)
	_, _, ok = PickCreative(s, 2160)
	require.False(t, ok)
	list := ListDouyin(body)
	require.Len(t, list, 1)
	require.Equal(t, s.ID, list[0].ID)
}

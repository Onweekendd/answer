/*
 * Licensed to the Apache Software Foundation (ASF) under one
 * or more contributor license agreements.  See the NOTICE file
 * distributed with this work for additional information
 * regarding copyright ownership.  The ASF licenses this file
 * to you under the Apache License, Version 2.0 (the
 * "License"); you may not use this file except in compliance
 * with the License.  You may obtain a copy of the License at
 *
 *   http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing,
 * software distributed under the License is distributed on an
 * "AS IS" BASIS, WITHOUT WARRANTIES OR CONDITIONS OF ANY
 * KIND, either express or implied.  See the License for the
 * specific language governing permissions and limitations
 * under the License.
 */

// @vitest-environment jsdom
import { afterEach, describe, expect, it } from "vitest";

import { genericList } from "../harvest/list";
import { profileFor, StepFailures, withFallback } from "./index";
import { taobao } from "./taobao";

afterEach(() => {
  document.body.innerHTML = "";
});

// card 照 2026-10-21 用 inspect 看到的搜索页结构写（类名后面的 hash 是构建产物，照抄几个）；商品、店铺、id 都是编的
function card(o: { id: string; href: string; title: string; price: string; desc?: string; shop?: string }): string {
  return `<div class="tbpc-col search-content-col">
    <a id="item_id_${o.id}" class="doubleCardWrapperAdapt--mEcC7olq" href="${o.href}"><div class="doubleCard--gO3Bz6bu">
      <div class="mainPicAndDesc--Q5PYrWux">
        <div class="imageSwitch--fJ9SrtEb"><img class="mainImg--sPh_U37m" src="https://img.test/${o.id}.jpg"></div>
        <div class="descWrapper--DYCP0aQA"><div class="title--ASSt27UY"><span>${o.title}</span></div></div>
        <div class="container--Mse0invQ"><div class="structuredWrapper--M1po5RF8">
          <div class="rankTitleWrapper--AeHheU61"><div class="rankTitle--yz_cx9ah">热水器热卖</div>榜·第5名</div>
          <div class="descBox--RunOO4S3"><span class="text--eAiSCa_r">60L</span><span class="text--eAiSCa_r">650x310x560mm</span></div>
        </div></div>
        <div class="priceWrapper--dBtPZ2K1"><div class="innerNormalPriceWrapper--tlX08mH1">
          <span class="unit--D3KGoZe2">¥</span>
          <div class="innerPriceWrapper--aAJhHXD4"><div class="priceInt--yqqZMJ5a">${o.price}</div><div class="priceFloat--XpixvyQ1"></div></div>
          ${o.desc ? `<span class="priceDesc--jykuwuhx">${o.desc}</span>` : ""}
          <span class="realSales--XZJiepmt">8000+人付款</span>
          <div class="procity--wlcT2xH9"><span>江苏</span></div>
        </div></div>
      </div>
      <div class="subIconWrapper--Vl8zAdQn"><div><span>直接下单享优惠</span></div></div>
      <div class="shopInfo--Kmh31boz"><span class="shopName--hdF527QA">
        <div class="shopTagWrapper--VTYE2RN6"><span>品牌旗舰</span></div>
        <span class="shopNameText--DmtlsDKm">${o.shop ?? "某品牌官方旗舰店"}</span>
      </span></div>
    </div></a>
  </div>`;
}

const tmall = card({
  id: "781000000001",
  href: "//detail.tmall.com/item.htm?id=781000000001&ali_refid=a%3A1&spm=a21n57.1",
  title: "某品牌电热水器 60L 一级能效",
  price: "7??",
  desc: "补贴后"
});
const ad = card({ id: "707000000002", href: "https://click.simba.taobao.com/cc_im?p=%E7%83%AD", title: "推广的热水器", price: "599" });
const taobaoShop = card({
  id: "650000000003",
  href: "//item.taobao.com/item.htm?id=650000000003&ns=1",
  title: "小店热水器",
  price: "499",
  shop: "某某家电专营"
});

describe("taobao 列表收割", () => {
  const steps = () => withFallback("list", taobao.list, genericList, new StepFailures());

  it("每个 a#item_id_… 是一张卡片，直通车广告认得出来", () => {
    // Arrange
    document.body.innerHTML = `<div id="content_items_wrapper">${ad}${tmall}${taobaoShop}</div>`;
    const s = steps();

    // Act
    const cards = s.cards(document);

    // Assert
    expect(profileFor(new URL("https://s.taobao.com/search?q=热水器"))?.name).toBe("taobao-search");
    expect(cards.map((c) => c.id)).toEqual(["item_id_707000000002", "item_id_781000000001", "item_id_650000000003"]);
    expect(cards.map((c) => s.sponsored(c))).toEqual([true, false, false]);
  });

  it("标题拼成一行：标题 ｜ 规格 ｜ 价 ｜ 销量 ｜ 店铺，榜单和「直接下单享优惠」不混进来", () => {
    // Arrange
    document.body.innerHTML = tmall;
    const [c] = steps().cards(document);

    // Act
    const title = steps().title(c);

    // Assert
    expect(title).toBe("某品牌电热水器 60L 一级能效 ｜ 60L · 650x310x560mm ｜ ¥7?? 补贴后 ｜ 8000+人付款 ｜ 品牌旗舰 某品牌官方旗舰店");
  });

  it("链接只留商品 id，天猫和淘宝各回各的域名；图是主图，去重用商品 id", () => {
    // Arrange
    document.body.innerHTML = `${tmall}${taobaoShop}`;
    const s = steps();
    const [t, b] = s.cards(document);

    // Act + Assert
    expect(s.link(t)).toBe("https://detail.tmall.com/item.htm?id=781000000001");
    expect(s.link(b)).toBe("https://item.taobao.com/item.htm?id=650000000003");
    expect(s.image(t)?.getAttribute("src")).toBe("https://img.test/781000000001.jpg");
    expect(s.key(t)).toBe("tb:781000000001");
  });
});

(() => {
  "use strict";

  const grid = document.querySelector("#work-grid");
  const status = document.querySelector("#collection-status");
  const filters = [...document.querySelectorAll("[data-filter]")];
  const randomButton = document.querySelector("#random-work");
  const details = {
    "kings-canyon": {
      order: -2,
      label: "KING’S CANYON / 3D EXPLORER",
      description: "从基地出发，沿河道散步，或飞到峡谷上空。换一个视角，探索熟悉的三路与野区。",
      tags: ["3D 漫游", "键鼠 / 触屏"],
      style: "canyon",
      note: "新作登场",
    },
    "chick-chick": {
      order: -1,
      label: "CHICK CHICK / VOXEL ADVENTURE",
      description:
        "小鸡向前冲！穿过车流、铁轨和河流，收集金币，看看这次能走多远。",
      tags: ["3D 过马路", "键盘 / 触屏"],
      style: "chick",
      note: "新作登场",
    },
    "woolly-run": {
      order: 0,
      label: "MACARON ADVENTURE",
      description: "软乎乎的角色，硬核一点的冒险。跳跃、冲刺，和朋友一起闯关。",
      tags: ["跑酷冒险", "单人 / 双人"],
      style: "woolly",
    },
    starcruiser: {
      order: 1,
      label: "STARCRUISER",
      description: "把战场搬到星空。驾驶战舰，在火力与护盾之间找到你的节奏。",
      tags: ["太空对战", "双人同屏"],
      style: "space",
    },
    "tankbattle-warriors": {
      order: 2,
      label: "TANK BATTLE / WARRIORS",
      description: "红绿两队，狭路相逢。找好掩体，和身边的朋友来一场像素对决。",
      tags: ["像素坦克", "双人对战"],
      style: "tank",
    },
    "tank-1v1": {
      order: 3,
      label: "PVP ARENA / 1V1",
      description: "一人一半屏幕，谁先拿下 10 分？移动、瞄准，把胜负留到最后。",
      tags: ["分屏竞技", "双人对战"],
      style: "arena",
    },
    towerfun: {
      order: 4,
      label: "TOWERFUN",
      description: "造塔防守，也派兵出击。把每一枚金币，花在扭转战局的地方。",
      tags: ["策略塔防", "1V1"],
      style: "tower",
    },
    "chujiao.vercel.app": {
      order: 5,
      label: "CHUJIAO / ON THE WEB",
      description: "游戏之外，也做一点网页。探索公司网站的界面、语言与风格。",
      tags: ["网页设计", "外部网站"],
      style: "web",
    },
  };

  function element(tag, className, text) {
    const node = document.createElement(tag);
    if (className) node.className = className;
    if (text !== undefined) node.textContent = text;
    return node;
  }

  function getWorks() {
    const groups = window.WEBPAGES_DATA?.pages?.["张牧作品"]?.["类型"];
    if (!Array.isArray(groups)) throw new Error("作品清单暂时没有加载成功");
    return groups
      .flatMap((group) =>
        Object.entries(group).flatMap(([category, items]) =>
          items.map((item) => {
            const address = item["地址"];
            const url = new URL(
              address.startsWith("/") || /^https?:\/\//.test(address)
                ? address
                : `https://${address}`,
              location.origin,
            );
            if (!["https:", "http:"].includes(url.protocol))
              throw new Error("作品链接格式有误");
            const slug =
              url.hostname === location.hostname
                ? url.pathname.split("/").filter(Boolean).pop()
                : url.hostname;
            return {
              title: item["名称"],
              url,
              slug,
              kind: category === "游戏" ? "game" : "web",
              feature: item["功能"],
              ...details[slug],
            };
          }),
        ),
      )
      .sort((a, b) => (a.order ?? 99) - (b.order ?? 99));
  }

  function makeCard(work, index) {
    const article = element("article", `work-card ${work.style || "web"}`);
    article.dataset.kind = work.kind;
    const link = element("a", "work-link");
    link.href = work.url.href;
    link.setAttribute("aria-labelledby", `work-title-${index}`);
    if (work.url.origin !== location.origin) {
      link.target = "_blank";
      link.rel = "noopener noreferrer";
    }

    const cover = element("div", "cover");
    const coverTop = element("div", "cover-top");
    coverTop.append(element("span", "cover-label", work.label || "MADE BY MU"));
    coverTop.append(
      element(
        "span",
        "cover-number",
        `${String(index + 1).padStart(2, "0")} / MU`,
      ),
    );
    cover.append(coverTop);

    if (work.style && work.style !== "web") {
      const image = element("img", "work-image");
      image.src = `/mu/previews/${work.slug}.png`;
      image.alt = `${work.title}${work.kind === "game" ? "游戏" : "场景"}实景`;
      image.width = 1200;
      image.height = 900;
      image.loading = index < 2 ? "eager" : "lazy";
      image.decoding = "async";
      image.addEventListener(
        "error",
        () => {
          image.remove();
          cover.append(element("span", "cover-fallback", work.title));
        },
        { once: true },
      );
      const frame = element("div", "preview-frame");
      frame.append(image);
      cover.append(frame);
    } else {
      const artwork = element("div", "web-art");
      artwork.setAttribute("aria-hidden", "true");
      if (work.slug === "chujiao.vercel.app") {
        const logo = element("img", "web-logo");
        logo.src = "/mu/previews/chujiao-logo-user.png";
        logo.alt = "触角公司 Logo";
        logo.width = 128;
        logo.height = 128;
        artwork.append(logo);
      } else {
        artwork.append(element("span", "web-word", "HELLO."));
      }
      artwork.append(element("span", "web-orbit"));
      artwork.append(
        element("span", "web-caption", "A DIFFERENT POINT OF VIEW"),
      );
      cover.append(artwork);
    }
    if (work.note)
      cover.append(element("span", "cover-note", work.note + " ↗"));
    const action = element(
      "span",
      "cover-action",
      work.kind === "game" ? "进入游戏 ↗" : "访问网站 ↗",
    );
    action.setAttribute("aria-hidden", "true");
    cover.append(action);

    const info = element("div", "work-info");
    const heading = element("div", "work-heading");
    const title = element("h3", "", work.title);
    title.id = `work-title-${index}`;
    const arrow = element("span", "work-arrow", "↗");
    arrow.setAttribute("aria-hidden", "true");
    heading.append(title, arrow);
    info.append(
      heading,
      element("p", "work-description", work.description || work.feature),
    );
    const tags = element("div", "work-tags");
    for (const tag of work.tags || [work.kind === "game" ? "游戏" : "网页"])
      tags.append(element("span", "", tag));
    if (link.target) tags.append(element("span", "sr-only", "在新标签页打开"));
    info.append(tags);
    link.append(cover, info);
    article.append(link);
    return article;
  }

  try {
    const works = getWorks();
    const cards = works.map(makeCard);
    grid.replaceChildren(...cards);
    grid.setAttribute("aria-busy", "false");
    document.querySelector("#total-count").textContent = String(
      works.length,
    ).padStart(2, "0");
    for (const count of document.querySelectorAll("[data-count]")) {
      count.textContent = works.filter(
        (work) =>
          count.dataset.count === "all" || work.kind === count.dataset.count,
      ).length;
    }
    const announce = (count) => {
      status.textContent = count
        ? `共 ${count} 个作品`
        : "这个分类还没有作品，试试其他分类。";
    };
    announce(works.length);
    for (const button of filters) {
      button.addEventListener("click", () => {
        const kind = button.dataset.filter;
        for (const filter of filters)
          filter.setAttribute("aria-pressed", String(filter === button));
        for (const card of cards)
          card.hidden = kind !== "all" && card.dataset.kind !== kind;
        announce(cards.filter((card) => !card.hidden).length);
      });
    }
    const games = works.filter((work) => work.kind === "game");
    randomButton.disabled = games.length === 0;
    randomButton.addEventListener("click", () => {
      if (games.length)
        location.assign(
          games[Math.floor(Math.random() * games.length)].url.href,
        );
    });
    if (!works.length)
      grid.append(element("p", "fallback", "作品正在准备中，过一阵再来看看。"));
  } catch (error) {
    const message = element("div", "fallback");
    message.append(element("p", "", "作品清单暂时没有加载成功。"));
    const retry = element("a", "text-link", "重新加载 ↗");
    retry.href = "/mu/";
    message.append(retry);
    grid.replaceChildren(message);
    grid.setAttribute("aria-busy", "false");
    filters.forEach((button) => {
      button.disabled = true;
    });
    status.textContent = "作品清单加载失败，请重新加载。";
  }
})();

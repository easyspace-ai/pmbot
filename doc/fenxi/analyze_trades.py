import csv
import glob
import os
from typing import List, Dict, Any, Optional, Tuple


TS_COL = "时间戳"
TIME_COL = "时间"
ACTION_COL = "动作"
DIR_COL = "方向"
MARKET_COL = "市场"
PRICE_COL = "价格"
QTY_COL = "数量"
USDC_COL = "USDC金额"
OUTCOME_IDX_COL = "OutcomeIndex"

UP_TOTAL_COST_COL = "up总成本"
UP_QTY_COL = "up持仓量"
UP_AVG_COL = "up均价"
DOWN_TOTAL_COST_COL = "down总成本"
DOWN_QTY_COL = "down持仓量"
DOWN_AVG_COL = "down均价"
UP_PROFIT_COL = "UP胜利润"
DOWN_PROFIT_COL = "DOWN胜利润"
PERIOD_COL = "时间段"


def parse_cycle_start_from_filename(path: str) -> Optional[int]:
    """
    从文件名中提取周期开始时间戳，如:
    bot_v0_8_cycle_1765382400.csv -> 1765382400
    """
    base = os.path.basename(path)
    name, _ext = os.path.splitext(base)
    parts = name.split("_")
    if not parts:
        return None
    last = parts[-1]
    try:
        return int(last)
    except ValueError:
        return None


def load_rows(path: str) -> Tuple[List[Dict[str, Any]], List[str]]:
    with open(path, "r", encoding="utf-8", newline="") as f:
        reader = csv.DictReader(f)
        rows: List[Dict[str, Any]] = [dict(r) for r in reader]
        headers = reader.fieldnames or []
    return rows, headers


def first_pass_totals(rows: List[Dict[str, Any]]) -> Tuple[float, float, float, float, Optional[float]]:
    """
    先遍历一遍，得到最终的总成本/持仓和最后一个UP价格
    """
    up_total_cost = 0.0
    up_total_qty = 0.0
    down_total_cost = 0.0
    down_total_qty = 0.0
    last_up_price: Optional[float] = None

    for r in rows:
        try:
            direction = r[DIR_COL]
        except KeyError:
            # 非预期格式，直接跳出
            break

        try:
            qty = float(r[QTY_COL])
            cost = float(r[USDC_COL])
        except (KeyError, ValueError, TypeError):
            continue

        if direction == "Up":
            up_total_cost += cost
            up_total_qty += qty
            try:
                last_up_price = float(r[PRICE_COL])
            except (KeyError, ValueError, TypeError):
                pass
        elif direction == "Down":
            down_total_cost += cost
            down_total_qty += qty

    return up_total_cost, up_total_qty, down_total_cost, down_total_qty, last_up_price


def decide_profit(
    up_total_cost: float,
    up_total_qty: float,
    down_total_cost: float,
    down_total_qty: float,
    last_up_price: Optional[float],
) -> float:
    """
    根据最后一个UP价格决定哪一边赢，并计算整个周期的利润:
    - 如果 last_up_price >= 0.50 则 UP 赢
    - 否则 DOWN 赢
    利润 = 获胜方向持仓量 * 1 - up总成本 - down总成本
    """
    # 如果没有UP成交，则认为UP价格 < 0.5，DOWN获胜
    up_wins = last_up_price is not None and last_up_price >= 0.50

    if up_wins:
        profit = up_total_qty * 1.0 - up_total_cost - down_total_cost
    else:
        profit = down_total_qty * 1.0 - down_total_cost - up_total_cost

    return profit


def classify_period(ts: int, cycle_start: Optional[int]) -> str:
    """
    按时间戳划分为三个阶段:
    - 0-5分钟: 基础建仓
    - 5-10分钟: 调整仓位、加仓、对冲
    - 10-15分钟: 继续调整仓位、锁定方向、对冲
    """
    if cycle_start is None:
        return "未知"

    delta = ts - cycle_start
    if delta < 0:
        return "未知"

    if delta < 300:
        return "0-5分钟"
    elif delta < 600:
        return "5-10分钟"
    else:
        return "10-15分钟"


def second_pass_with_columns(
    rows: List[Dict[str, Any]],
    cycle_start: Optional[int],
) -> List[Dict[str, Any]]:
    """
    第二次遍历，计算每一行的累计持仓相关字段，
    并基于“当前时刻的持仓与成本”动态计算：
      - 如果此刻UP胜，会赚多少 (UP胜利润)
      - 如果此刻DOWN胜，会赚多少 (DOWN胜利润)
    """
    up_total_cost_running = 0.0
    up_total_qty_running = 0.0
    down_total_cost_running = 0.0
    down_total_qty_running = 0.0

    new_rows: List[Dict[str, Any]] = []

    for r in rows:
        new_r = dict(r)
        try:
            direction = r[DIR_COL]
            qty = float(r[QTY_COL])
            cost = float(r[USDC_COL])
        except (KeyError, ValueError, TypeError):
            direction = ""
            qty = 0.0
            cost = 0.0

        if direction == "Up":
            up_total_cost_running += cost
            up_total_qty_running += qty
        elif direction == "Down":
            down_total_cost_running += cost
            down_total_qty_running += qty

        # 运行中的均价
        up_avg = up_total_cost_running / up_total_qty_running if up_total_qty_running > 0 else ""
        down_avg = down_total_cost_running / down_total_qty_running if down_total_qty_running > 0 else ""

        # 时间段
        try:
            ts = int(r[TS_COL])
        except (KeyError, ValueError, TypeError):
            ts = 0
        period = classify_period(ts, cycle_start)

        new_r[UP_TOTAL_COST_COL] = f"{up_total_cost_running:.8f}" if up_total_qty_running > 0 else ""
        new_r[UP_QTY_COL] = f"{up_total_qty_running:.8f}" if up_total_qty_running > 0 else ""
        new_r[UP_AVG_COL] = f"{up_avg:.8f}" if up_total_qty_running > 0 else ""

        new_r[DOWN_TOTAL_COST_COL] = f"{down_total_cost_running:.8f}" if down_total_qty_running > 0 else ""
        new_r[DOWN_QTY_COL] = f"{down_total_qty_running:.8f}" if down_total_qty_running > 0 else ""
        new_r[DOWN_AVG_COL] = f"{down_avg:.8f}" if down_total_qty_running > 0 else ""

        # 动态利润：假设此刻立刻结算
        if up_total_qty_running > 0 or down_total_qty_running > 0:
            up_profit = (
                up_total_qty_running * 1.0
                - up_total_cost_running
                - down_total_cost_running
            )
            down_profit = (
                down_total_qty_running * 1.0
                - down_total_cost_running
                - up_total_cost_running
            )
            new_r[UP_PROFIT_COL] = f"{up_profit:.8f}"
            new_r[DOWN_PROFIT_COL] = f"{down_profit:.8f}"
        else:
            new_r[UP_PROFIT_COL] = ""
            new_r[DOWN_PROFIT_COL] = ""

        new_r[PERIOD_COL] = period

        new_rows.append(new_r)

    return new_rows


def process_file(path: str) -> None:
    rows, headers = load_rows(path)
    if not rows or not headers:
        print(f"跳过空文件: {path}")
        return

    required_cols = {TS_COL, DIR_COL, PRICE_COL, QTY_COL, USDC_COL}
    if not required_cols.issubset(headers):
        print(f"文件缺少必要列, 跳过: {path}")
        return

    cycle_start = parse_cycle_start_from_filename(path)

    up_total_cost, up_total_qty, down_total_cost, down_total_qty, last_up_price = first_pass_totals(rows)

    # 按结算规则计算整个周期的真实利润（只用于日志）
    cycle_profit = decide_profit(
        up_total_cost, up_total_qty, down_total_cost, down_total_qty, last_up_price
    )

    # 基于每一行“当前时刻”的持仓和成本，动态计算UP胜/ DOWN胜利润
    new_rows = second_pass_with_columns(rows, cycle_start)

    # 输出文件名: 在原文件名后增加 _analyzed
    base = os.path.basename(path)
    name, ext = os.path.splitext(base)
    out_name = f"{name}_analyzed{ext}"
    out_path = os.path.join(os.path.dirname(path), out_name)

    # 新表头 = 原表头 + 新增列
    new_headers = list(headers) + [
        UP_TOTAL_COST_COL,
        UP_QTY_COL,
        UP_AVG_COL,
        DOWN_TOTAL_COST_COL,
        DOWN_QTY_COL,
        DOWN_AVG_COL,
        UP_PROFIT_COL,
        DOWN_PROFIT_COL,
        PERIOD_COL,
    ]

    with open(out_path, "w", encoding="utf-8", newline="") as f:
        writer = csv.DictWriter(f, fieldnames=new_headers)
        writer.writeheader()
        for r in new_rows:
            writer.writerow(r)

    print(
        f"处理完成: {base}, 周期真实利润={cycle_profit:.8f}, "
        f"UP持仓={up_total_qty:.4f}, DOWN持仓={down_total_qty:.4f}"
    )


def main() -> None:
    script_dir = os.path.dirname(os.path.abspath(__file__))
    pattern = os.path.join(script_dir, "bot_v0_8_cycle_*.csv")
    files = sorted(glob.glob(pattern))
    if not files:
        print(f"未找到匹配的CSV文件: {pattern}")
        return

    for path in files:
        process_file(path)


if __name__ == "__main__":
    main()



var BillingController = (function() {
	function round(num) {
		return Math.round(num * 1000) / 1000
	}

	var my = {
		init: function() {
			$.ajax({
				type: 'POST',
				dataType: 'json',
				url: cAPIBase + '/billing_info',
				data: {
					u: LoginController.getUser(),
					k: LoginController.getKey(),
				},
				success: function(data) {
					var billingTable = $('#billing-pending-table-body');
					var billsTable = $('#billing-history-table-body');
					$('#month-do-date-cost').text('$' + round(data.to_pay));
					console.log('Result');
					console.log(data);
					console.log(data.history);
					var i = 0;
					$.each(data.history.reverse(), function(_, line) {
						if (line.paid) {
							var paidClass = 'billing_row_paid';
						} else {
							var paidClass = 'billing_row_nopaid';
						}
						billingTable.append($("\
							<tr class=\"" + paidClass + "\">\
								<th scope=\"row\">" + (data.history.length - ++i) + "</th>\
								<td>" + line.group + "</td>\
								<td>" + line.instances + "</td>\
								<td>" + line.type + "</td>\
								<td>" + new Date(line.from * 1000) + "</td>\
								<td>" + new Date(line.to * 1000) + "</td>\
								<td>" + round((line.to - line.from) / 3600) + "</td>\
								<td>" + round(line.price) + "</td>\
							</tr>\
						"));
					});

					i = 0;
					$.each(data.bills.reverse(), function(_, line) {
						var paypalButton = '';
						if (!line.paid) {
							// TODO Add paypal button
						}
						billsTable.append($("\
							<tr>\
								<th scope=\"row\">" + (data.bills.length - ++i) + "</th>\
								<td>" + new Date(line.from) + "</td>\
								<td>" + new Date(line.to) + "</td>\
								<td>" + line.amount + "</td>\
								<td>" + line.paid + " " + paypalButton + "</td>\
							</tr>\
						"));
					});
				},
				error: function() {
					alert("The billing information can't be read");
				},
			});
		}
	};

	return my;
})();
